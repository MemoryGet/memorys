// Package accessgrant verifies a dedicated signed module grant. It does not
// authenticate the calling service or authorize current knowledge access.
package accessgrant

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/MemoryGet/memorys/pkg/canonical"
	jose "github.com/go-jose/go-jose/v4"
)

var ErrInvalid = errors.New("invalid access grant")
var pathPattern = regexp.MustCompile(`^/modules/v1/[a-z_]+/[a-z_]+$`)
var integerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

type Context struct {
	Format           string `json:"format"`
	IssuerID         string `json:"issuer_id"`
	AudienceModuleID string `json:"audience_module_id"`
	CallerServiceID  string `json:"caller_service_id"`
	SubjectID        string `json:"subject_id"`
	Action           string `json:"action"`
	Method           string `json:"method"`
	Path             string `json:"path"`
	InvocationID     string `json:"invocation_id"`
	RequestDigest    string `json:"request_digest"`
	ScopeDigest      string `json:"scope_digest"`
	ProfileDigest    string `json:"profile_digest"`
}

type Bindings struct {
	Context
	IssuedAtNS  int64 `json:"issued_at_ns"`
	NotBeforeNS int64 `json:"not_before_ns"`
	ExpiresAtNS int64 `json:"expires_at_ns"`
}

type Claims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  string   `json:"aud"`
	TokenID   string   `json:"jti"`
	IssuedAt  int64    `json:"iat"`
	NotBefore int64    `json:"nbf"`
	ExpiresAt int64    `json:"exp"`
	Bindings  Bindings `json:"akos"`
}

// Trust is an immutable deployment snapshot, never constructed from token URLs.
// Callers must not mutate the map or key bytes concurrently with verification.
type Trust struct {
	IssuerID      string
	PublicKeys    map[string]ed25519.PublicKey
	MaxLifetimeNS int64
}

type Expectation struct {
	Context
	DeadlineNS     int64
	ScopeExpiresNS int64
}

func strict(raw []byte, out any) error {
	encoded, err := canonical.JSON(raw)
	if err != nil || len(encoded) == 0 || encoded[0] != '{' {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return ErrInvalid
	}
	return nil
}

func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func fields(raw []byte, names ...string) (map[string]json.RawMessage, bool) {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != len(names) {
		return nil, false
	}
	for _, name := range names {
		if _, ok := values[name]; !ok {
			return nil, false
		}
	}
	return values, true
}

func protectedKeyID(token string) (string, error) {
	if len(token) > 16<<10 || strings.Count(token, ".") != 2 {
		return "", ErrInvalid
	}
	part := strings.SplitN(token, ".", 2)[0]
	raw, err := base64.RawURLEncoding.Strict().DecodeString(part)
	if err != nil || len(raw) > 2<<10 {
		return "", ErrInvalid
	}
	var h struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		KeyID     string `json:"kid"`
	}
	if strict(raw, &h) != nil {
		return "", ErrInvalid
	}
	return h.KeyID, nil
}

func validContext(c Context) bool {
	if c.Format != "akos.grant-bindings/1" || len(c.Path) > 200 || !pathPattern.MatchString(c.Path) {
		return false
	}
	if c.Method != "POST" && c.Method != "PUT" && c.Method != "GET" && c.Method != "DELETE" {
		return false
	}
	for _, v := range []string{c.IssuerID, c.AudienceModuleID, c.CallerServiceID, c.SubjectID, c.Action, c.InvocationID} {
		if v == "" || len(v) > 200 {
			return false
		}
	}
	return digest(c.RequestDigest) && digest(c.ScopeDigest) && digest(c.ProfileDigest)
}

// Verify returns signature-verified, request-bound claims only. Current policy,
// operation/action rules, task ownership and replay ledger remain mandatory.
func Verify(token string, trust Trust, expected Expectation, nowNS int64) (Claims, error) {
	fail := func() (Claims, error) { return Claims{}, ErrInvalid }
	if len(token) == 0 || len(token) > 16<<10 || strings.Count(token, ".") != 2 || trust.MaxLifetimeNS <= 0 {
		return fail()
	}
	parts := strings.SplitN(token, ".", 3)
	var decoded [3][]byte
	for i, p := range parts {
		value, err := base64.RawURLEncoding.Strict().DecodeString(p)
		if err != nil || len(value) == 0 || base64.RawURLEncoding.EncodeToString(value) != p {
			return fail()
		}
		decoded[i] = value
	}
	if len(decoded[0]) > 2<<10 || len(decoded[1]) > 8<<10 || len(decoded[2]) != ed25519.SignatureSize {
		return fail()
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		KeyID     string `json:"kid"`
	}
	if strict(decoded[0], &header) != nil || header.Algorithm != "EdDSA" || header.Type != "akos-access-grant+jwt" || header.KeyID == "" || len(header.KeyID) > 200 {
		return fail()
	}
	if _, ok := fields(decoded[0], "alg", "typ", "kid"); !ok {
		return fail()
	}
	key, ok := trust.PublicKeys[header.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return fail()
	}
	// The SDK owns signature parsing and cryptographic verification.
	signed, err := jose.ParseSignedCompact(token, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		return fail()
	}
	payload, err := signed.Verify(append(ed25519.PublicKey(nil), key...))
	if err != nil {
		return fail()
	}
	var claims Claims
	if strict(payload, &claims) != nil {
		return fail()
	}
	outer, ok := fields(payload, "iss", "sub", "aud", "jti", "iat", "nbf", "exp", "akos")
	if !ok {
		return fail()
	}
	inner, ok := fields(outer["akos"], "format", "issuer_id", "audience_module_id", "caller_service_id", "subject_id", "action", "method", "path", "invocation_id", "request_digest", "scope_digest", "profile_digest", "issued_at_ns", "not_before_ns", "expires_at_ns")
	if !ok {
		return fail()
	}
	for _, name := range []string{"iat", "nbf", "exp"} {
		if !integerPattern.Match(bytes.TrimSpace(outer[name])) {
			return fail()
		}
	}
	for _, name := range []string{"issued_at_ns", "not_before_ns", "expires_at_ns"} {
		if !integerPattern.Match(bytes.TrimSpace(inner[name])) {
			return fail()
		}
	}
	b := claims.Bindings
	if !validContext(b.Context) || b.Context != expected.Context || trust.IssuerID != b.IssuerID {
		return fail()
	}
	if claims.Issuer != b.IssuerID || claims.Subject != b.SubjectID || claims.Audience != b.AudienceModuleID || claims.TokenID == "" || len(claims.TokenID) > 200 {
		return fail()
	}
	if nowNS < 0 || b.IssuedAtNS < 0 || b.IssuedAtNS > b.NotBeforeNS || b.NotBeforeNS > nowNS || nowNS >= b.ExpiresAtNS {
		return fail()
	}
	if nowNS >= expected.DeadlineNS || expected.DeadlineNS > expected.ScopeExpiresNS || b.ExpiresAtNS > expected.DeadlineNS || b.ExpiresAtNS > expected.ScopeExpiresNS {
		return fail()
	}
	if b.ExpiresAtNS-b.IssuedAtNS > trust.MaxLifetimeNS {
		return fail()
	}
	expSeconds := b.ExpiresAtNS / 1_000_000_000
	if b.ExpiresAtNS%1_000_000_000 != 0 {
		expSeconds++
	}
	if claims.IssuedAt != b.IssuedAtNS/1_000_000_000 || claims.NotBefore != b.NotBeforeNS/1_000_000_000 || claims.ExpiresAt != expSeconds {
		return fail()
	}
	return claims, nil
}

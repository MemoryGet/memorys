// Package identitytoken authenticates a restricted OAuth access-token profile.
// Permission decisions and module invocation grants remain separate services.
package identitytoken

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	jose "github.com/go-jose/go-jose/v4"
)

var ErrAuthentication = errors.New("access token authentication failed")
var integerDate = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// StatusSource must check current issuer/key, subject, client and token status
// against a trusted source, not derive activity from these argument claims.
type StatusSource interface {
	Active(context.Context, StatusQuery) (bool, error)
}
type StatusQuery struct {
	Issuer, KeyID, SubjectID, ClientID, TokenID, SubjectType string
	PublicKeyDigest                                          string
	IssuedAt, ExpiresAt                                      int64
}
type Config struct {
	Issuer, Audience, SubjectType string
	Keys                          map[string]*rsa.PublicKey
	MaxLifetime                   time.Duration
}
type Authenticator struct {
	issuer, audience, subjectType string
	keys                          map[string]*rsa.PublicKey
	lifetime                      int64
	status                        StatusSource
}

var _ contracts.Authenticator = (*Authenticator)(nil)

func valid(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value)
}
func New(c Config, status StatusSource) (*Authenticator, error) {
	if !valid(c.Issuer, 2048) || !valid(c.Audience, 2048) || (c.SubjectType != "user" && c.SubjectType != "service") || status == nil || reflect.ValueOf(status).Kind() == reflect.Pointer && reflect.ValueOf(status).IsNil() || c.MaxLifetime < time.Second || c.MaxLifetime > 24*time.Hour || c.MaxLifetime%time.Second != 0 || len(c.Keys) == 0 || len(c.Keys) > 100 {
		return nil, ErrAuthentication
	}
	p := &Authenticator{issuer: c.Issuer, audience: c.Audience, subjectType: c.SubjectType, keys: map[string]*rsa.PublicKey{}, lifetime: int64(c.MaxLifetime / time.Second), status: status}
	for id, key := range c.Keys {
		if !valid(id, 200) || key == nil || key.N == nil || key.N.Sign() <= 0 || key.N.BitLen() < 2048 || key.N.BitLen() > 8192 || key.E != 65537 {
			return nil, ErrAuthentication
		}
		p.keys[id] = &rsa.PublicKey{N: new(big.Int).Set(key.N), E: key.E}
	}
	return p, nil
}

func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	if _, err := canonical.JSON(raw); err != nil {
		return nil, ErrAuthentication
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, ErrAuthentication
	}
	return object, nil
}
func claimString(m map[string]json.RawMessage, name string, max int) (string, bool) {
	var value string
	if json.Unmarshal(m[name], &value) != nil || !valid(value, max) {
		return "", false
	}
	return value, true
}
func date(m map[string]json.RawMessage, name string) (int64, bool) {
	raw := m[name]
	if !integerDate.Match(raw) {
		return 0, false
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return n, err == nil && n <= 253402300799
}

func (p *Authenticator) Authenticate(ctx context.Context, credential contracts.Credential) (contracts.Principal, error) {
	var empty contracts.Principal
	if p == nil || ctx == nil || p.status == nil || credential.Kind != "access_token" || len(credential.Secret) == 0 || len(credential.Secret) > 16<<10 || ctx.Err() != nil {
		return empty, ErrAuthentication
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrAuthentication
	}
	parts := strings.Split(string(credential.Secret), ".")
	if len(parts) != 3 {
		return empty, ErrAuthentication
	}
	var decoded [3][]byte
	for i, part := range parts {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(part)
		if err != nil || len(raw) == 0 || base64.RawURLEncoding.EncodeToString(raw) != part {
			return empty, ErrAuthentication
		}
		decoded[i] = raw
	}
	if len(decoded[0]) > 2048 || len(decoded[1]) > 12<<10 {
		return empty, ErrAuthentication
	}
	header, err := strictObject(decoded[0])
	if err != nil || len(header) != 3 {
		return empty, ErrAuthentication
	}
	alg, a := claimString(header, "alg", 20)
	typ, t := claimString(header, "typ", 100)
	kid, k := claimString(header, "kid", 200)
	if !a || !t || !k || alg != "RS256" || (typ != "at+jwt" && typ != "application/at+jwt") {
		return empty, ErrAuthentication
	}
	key, ok := p.keys[kid]
	if !ok || len(decoded[2]) != key.Size() {
		return empty, ErrAuthentication
	}
	signed, err := jose.ParseSignedCompact(string(credential.Secret), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return empty, ErrAuthentication
	}
	payload, err := signed.Verify(key)
	if err != nil {
		return empty, ErrAuthentication
	}
	claims, err := strictObject(payload)
	if err != nil {
		return empty, ErrAuthentication
	}
	for _, unsupported := range []string{"act", "may_act", "cnf"} {
		if _, found := claims[unsupported]; found {
			return empty, ErrAuthentication
		}
	}
	issuer, i := claimString(claims, "iss", 2048)
	subject, s := claimString(claims, "sub", 200)
	client, c := claimString(claims, "client_id", 200)
	tokenID, j := claimString(claims, "jti", 200)
	iat, issued := date(claims, "iat")
	exp, expires := date(claims, "exp")
	if !i || !s || !c || !j || !issued || !expires || issuer != p.issuer || exp <= iat || exp-iat > p.lifetime {
		return empty, ErrAuthentication
	}
	var audiences []string
	if len(claims["aud"]) > 0 && claims["aud"][0] == '"' {
		var audience string
		if json.Unmarshal(claims["aud"], &audience) != nil {
			return empty, ErrAuthentication
		}
		audiences = []string{audience}
	} else if json.Unmarshal(claims["aud"], &audiences) != nil {
		return empty, ErrAuthentication
	}
	if len(audiences) == 0 || len(audiences) > 16 {
		return empty, ErrAuthentication
	}
	matched := false
	seen := map[string]bool{}
	for _, audience := range audiences {
		if !valid(audience, 2048) || seen[audience] {
			return empty, ErrAuthentication
		}
		seen[audience] = true
		matched = matched || audience == p.audience
	}
	if !matched {
		return empty, ErrAuthentication
	}
	nbf := iat
	if _, found := claims["nbf"]; found {
		var valid bool
		nbf, valid = date(claims, "nbf")
		if !valid || nbf >= exp {
			return empty, ErrAuthentication
		}
	}
	current := func() bool {
		now := time.Now().Unix()
		return iat <= now && nbf <= now && now < exp && ctx.Err() == nil
	}
	if !current() {
		return empty, ErrAuthentication
	}
	keyBytes, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return empty, ErrAuthentication
	}
	fingerprint := sha256.Sum256(keyBytes)
	active, err := p.status.Active(ctx, StatusQuery{Issuer: issuer, KeyID: kid, SubjectID: subject, ClientID: client, TokenID: tokenID, SubjectType: p.subjectType, PublicKeyDigest: hex.EncodeToString(fingerprint[:]), IssuedAt: iat, ExpiresAt: exp})
	if err != nil || !active || !current() {
		return empty, ErrAuthentication
	}
	return contracts.Principal{SubjectID: contracts.ID(subject), SubjectType: p.subjectType, Issuer: issuer, ClientID: contracts.ID(client)}, nil
}

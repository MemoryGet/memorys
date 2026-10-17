package identitytoken_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	jose "github.com/go-jose/go-jose/v4"
)

var once sync.Once
var signingKey *rsa.PrivateKey
var signingError error

func key(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	once.Do(func() { signingKey, signingError = rsa.GenerateKey(rand.Reader, 2048) })
	if signingError != nil {
		t.Fatal(signingError)
	}
	return signingKey
}

type status struct {
	active bool
	err    error
	calls  int
	last   identitytoken.StatusQuery
}

func (s *status) Active(_ context.Context, q identitytoken.StatusQuery) (bool, error) {
	s.calls++
	s.last = q
	return s.active, s.err
}
func fixture(t *testing.T) (identitytoken.Config, map[string]any, *status) {
	k := key(t)
	now := time.Now().Unix()
	return identitytoken.Config{Issuer: "https://issuer.example", Audience: "knowledge-api", SubjectType: "user", MaxLifetime: time.Minute, Keys: map[string]*rsa.PublicKey{"key-1": {N: new(big.Int).Set(k.N), E: k.E}}}, map[string]any{"iss": "https://issuer.example", "sub": "subject", "client_id": "client", "aud": []string{"knowledge-api", "other-resource"}, "jti": "token-1", "iat": now - 2, "nbf": now - 1, "exp": now + 40}, &status{active: true}
}
func sign(t *testing.T, raw []byte, typ, kid string) contracts.Credential {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key(t)}, options)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return contracts.Credential{Kind: "access_token", Secret: []byte(token)}
}
func token(t *testing.T, c map[string]any) contracts.Credential {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return sign(t, raw, "at+jwt", "key-1")
}

func TestAccessTokenAuthenticatesSDKSignatureAndChecksFreshStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, claims, s := fixture(t)
	auth, err := identitytoken.New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	// Caller mutation cannot replace the trusted public key after construction.
	c.Keys["key-1"].N.SetInt64(3)
	claims["scope"] = "admin:all"
	claims["roles"] = []string{"owner"}
	credential := token(t, claims)
	p, err := auth.Authenticate(ctx, credential)
	if err != nil || p.SubjectID != "subject" || p.SubjectType != "user" || p.Issuer != "https://issuer.example" || p.ClientID != "client" || p.Delegation != nil || s.calls != 1 || s.last.TokenID != "token-1" || s.last.KeyID != "key-1" || s.last.SubjectType != "user" {
		t.Fatal("identity/status binding failed", err)
	}
	s.active = false
	if _, err := auth.Authenticate(ctx, credential); !errors.Is(err, identitytoken.ErrAuthentication) || s.calls != 2 {
		t.Fatal("cached token status accepted revoked identity")
	}
}

func TestAccessTokenRejectsWrongTypeKeySignatureAndUnboundedCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, claims, s := fixture(t)
	auth, err := identitytoken.New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(claims)
	for _, credential := range []contracts.Credential{sign(t, raw, "JWT", "key-1"), sign(t, raw, "akos-access-grant+jwt", "key-1"), sign(t, raw, "at+jwt", "unknown-key")} {
		if _, err := auth.Authenticate(ctx, credential); err == nil {
			t.Fatal("wrong token kind/key accepted")
		}
	}
	valid := token(t, claims)
	parts := strings.Split(string(valid.Secret), ".")
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[0] ^= 1
	parts[2] = base64.RawURLEncoding.EncodeToString(sig)
	if _, err := auth.Authenticate(ctx, contracts.Credential{Kind: "access_token", Secret: []byte(strings.Join(parts, "."))}); err == nil {
		t.Fatal("invalid signature accepted")
	}
	if _, err := auth.Authenticate(context.Background(), valid); err == nil {
		t.Fatal("unbounded authentication accepted")
	}
	if s.calls != 0 {
		t.Fatal("unverified token reached identity status source")
	}
}

func TestAccessTokenRejectsIssuerAudienceTimeAndUnsupportedBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, name := range []string{"issuer", "audience", "duplicate-audience", "expired", "future-iat", "future-nbf", "long-life", "no-client", "no-jti", "fractional-date", "delegation", "confirmation"} {
		t.Run(name, func(t *testing.T) {
			c, v, s := fixture(t)
			now := time.Now().Unix()
			switch name {
			case "issuer":
				v["iss"] = "other"
			case "audience":
				v["aud"] = []string{"other"}
			case "duplicate-audience":
				v["aud"] = []string{"knowledge-api", "knowledge-api"}
			case "expired":
				v["exp"] = now - 1
			case "future-iat":
				v["iat"] = now + 10
			case "future-nbf":
				v["nbf"] = now + 10
			case "long-life":
				v["exp"] = now + 100
			case "no-client":
				delete(v, "client_id")
			case "no-jti":
				delete(v, "jti")
			case "fractional-date":
				v["iat"] = float64(now) - 2.5
			case "delegation":
				v["act"] = map[string]string{"sub": "actor"}
			case "confirmation":
				v["cnf"] = map[string]string{"jkt": "key"}
			}
			auth, err := identitytoken.New(c, s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := auth.Authenticate(ctx, token(t, v)); err == nil || s.calls != 0 {
				t.Fatal("invalid access claims reached status source")
			}
		})
	}
}

func TestAccessTokenRejectsDuplicateClaimsAndStatusFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, v, s := fixture(t)
	auth, err := identitytoken.New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	raw = append([]byte(`{"sub":"other",`), raw[1:]...)
	if _, err := auth.Authenticate(ctx, sign(t, raw, "at+jwt", "key-1")); err == nil || s.calls != 0 {
		t.Fatal("duplicate identity claim accepted")
	}
	s.err = errors.New("private directory credential")
	if _, err := auth.Authenticate(ctx, token(t, v)); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("identity directory failure accepted/leaked")
	}
	var missing *status
	if _, err := identitytoken.New(c, missing); err == nil {
		t.Fatal("typed nil status provider accepted")
	}
}

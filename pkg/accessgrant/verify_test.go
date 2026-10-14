package accessgrant_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/accessgrant"
	jose "github.com/go-jose/go-jose/v4"
)

func fixture(t *testing.T) (ed25519.PrivateKey, accessgrant.Trust, accessgrant.Expectation, accessgrant.Claims) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := accessgrant.Context{Format: "akos.grant-bindings/1", IssuerID: "issuer", AudienceModuleID: "module", CallerServiceID: "service", SubjectID: "subject", Action: "knowledge.ask", Method: "POST", Path: "/modules/v1/generator/generate", InvocationID: "invoke", RequestDigest: strings.Repeat("a", 64), ScopeDigest: strings.Repeat("b", 64), ProfileDigest: strings.Repeat("c", 64)}
	trust := accessgrant.Trust{IssuerID: "issuer", PublicKeys: map[string]ed25519.PublicKey{"key-1": pub}, MaxLifetimeNS: 2_000_000_000}
	expected := accessgrant.Expectation{Context: c, DeadlineNS: 2_500_000_000, ScopeExpiresNS: 3_000_000_000}
	claims := accessgrant.Claims{Issuer: "issuer", Subject: "subject", Audience: "module", TokenID: "token-1", IssuedAt: 1, NotBefore: 1, ExpiresAt: 3, Bindings: accessgrant.Bindings{Context: c, IssuedAtNS: 1_100_000_000, NotBeforeNS: 1_100_000_000, ExpiresAtNS: 2_500_000_000}}
	return priv, trust, expected, claims
}

func sign(t *testing.T, priv ed25519.PrivateKey, raw []byte, typ, kid string) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: priv}, opts)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func raw(t *testing.T, claims accessgrant.Claims) []byte {
	t.Helper()
	b, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSDKSignatureAndEachTamperedSegment(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	token := sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	got, err := accessgrant.Verify(token, trust, want, 1_500_000_000)
	if err != nil || got != claims {
		t.Fatal("valid signed claims rejected", err)
	}
	for index := 0; index < 3; index++ {
		parts := strings.Split(token, ".")
		b, _ := base64.RawURLEncoding.DecodeString(parts[index])
		b[0] ^= 1
		parts[index] = base64.RawURLEncoding.EncodeToString(b)
		if got, err := accessgrant.Verify(strings.Join(parts, "."), trust, want, 1_500_000_000); err != accessgrant.ErrInvalid || got != (accessgrant.Claims{}) {
			t.Fatal("tampered token accepted", index)
		}
	}
}

func TestSignedWrongClaimsHeadersAndKeySnapshot(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	for _, mutation := range []func(*accessgrant.Claims){func(c *accessgrant.Claims) { c.Issuer = "other" }, func(c *accessgrant.Claims) { c.Bindings.Action = "index.delete" }, func(c *accessgrant.Claims) { c.Bindings.RequestDigest = strings.Repeat("d", 64) }, func(c *accessgrant.Claims) { c.Audience = "other" }, func(c *accessgrant.Claims) { c.ExpiresAt = 2 }} {
		c := claims
		mutation(&c)
		token := sign(t, priv, raw(t, c), "akos-access-grant+jwt", "key-1")
		if _, err := accessgrant.Verify(token, trust, want, 1_500_000_000); err == nil {
			t.Fatal("signed wrong claims accepted")
		}
	}
	for _, header := range [][2]string{{"JWT", "key-1"}, {"akos-access-grant+jwt", "unknown"}} {
		token := sign(t, priv, raw(t, claims), header[0], header[1])
		if _, err := accessgrant.Verify(token, trust, want, 1_500_000_000); err == nil {
			t.Fatal("wrong header accepted")
		}
	}
	token := sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	delete(trust.PublicKeys, "key-1")
	if _, err := accessgrant.Verify(token, trust, want, 1_500_000_000); err == nil {
		t.Fatal("removed key accepted")
	}
}

func TestNanosecondBoundaryAndMalformedSignedPayloads(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	token := sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	if _, err := accessgrant.Verify(token, trust, want, 2_499_999_999); err != nil {
		t.Fatal(err)
	}
	for _, now := range []int64{1_099_999_999, 2_500_000_000} {
		if _, err := accessgrant.Verify(token, trust, want, now); err == nil {
			t.Fatal("exact time boundary accepted")
		}
	}
	b := string(raw(t, claims))
	for _, bad := range []string{strings.Replace(b, `"iat":1`, `"iat":1.0`, 1), strings.Replace(b, `"iat":1`, `"iat":1,"iat":1`, 1), strings.Replace(b, `"iat":1,`, ``, 1), strings.Replace(b, `"iat":1`, `"IAT":1`, 1)} {
		token := sign(t, priv, []byte(bad), "akos-access-grant+jwt", "key-1")
		if _, err := accessgrant.Verify(token, trust, want, 1_500_000_000); err == nil {
			t.Fatal("malformed signed claims accepted")
		}
	}
	if _, err := accessgrant.Verify(strings.Repeat("x", 17<<10), trust, want, 1_500_000_000); err == nil {
		t.Fatal("oversized token accepted")
	}
}

func TestSDKAlgorithmConfusionAndNullEpochRejected(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	options := (&jose.SignerOptions{}).WithType("akos-access-grant+jwt").WithHeader("kid", "key-1")
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: make([]byte, 32)}, options)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := signer.Sign(raw(t, claims))
	if err != nil {
		t.Fatal(err)
	}
	token, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accessgrant.Verify(token, trust, want, 1_500_000_000); err == nil {
		t.Fatal("HS256 confused with EdDSA")
	}
	claims.IssuedAt = 0
	claims.NotBefore = 0
	claims.Bindings.IssuedAtNS = 0
	claims.Bindings.NotBeforeNS = 0
	trust.MaxLifetimeNS = 3_000_000_000
	b := string(raw(t, claims))
	b = strings.Replace(b, `"issued_at_ns":0`, `"issued_at_ns":null`, 1)
	token = sign(t, priv, []byte(b), "akos-access-grant+jwt", "key-1")
	if _, err := accessgrant.Verify(token, trust, want, 1_500_000_000); err == nil {
		t.Fatal("null interpreted as zero epoch")
	}
}

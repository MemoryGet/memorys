package accessgrant_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/MemoryGet/memorys/pkg/accessgrant"
)

func snapshotConfig(trust accessgrant.Trust) accessgrant.SnapshotConfig {
	return accessgrant.SnapshotConfig{SnapshotID: "trust-1", Version: 1, IssuerID: "issuer", ValidFromNS: 1_000_000_000, ValidUntilNS: 4_000_000_000, MaxLifetimeNS: 3_000_000_000,
		Keys: []accessgrant.KeyConfig{{KeyID: "key-1", Algorithm: "EdDSA", PublicKey: trust.PublicKeys["key-1"], EnabledAtNS: 1_000_000_000, SignUntilNS: 1_500_000_000, VerifyUntilNS: 3_000_000_000}}}
}

func TestSnapshotOwnsKeysAndNormalOverlap(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	config := snapshotConfig(trust)
	newPub, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config.Keys = append(config.Keys, accessgrant.KeyConfig{KeyID: "key-2", Algorithm: "EdDSA", PublicKey: newPub, EnabledAtNS: 1_000_000_000, SignUntilNS: 3_000_000_000, VerifyUntilNS: 4_000_000_000})
	v, err := accessgrant.NewVerifier(config)
	if err != nil {
		t.Fatal(err)
	}
	oldToken := sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	newToken := sign(t, newPrivate, raw(t, claims), "akos-access-grant+jwt", "key-2")
	for _, token := range []string{oldToken, newToken} {
		if _, err := v.Verify(token, want, 2_000_000_000); err != nil {
			t.Fatal("overlap rejected", err)
		}
	}
	identity := v.Identity()
	config.Keys[0].PublicKey[0] ^= 1
	config.Keys[0].VerifyUntilNS = 1
	config.IssuerID = "mutated"
	if _, err := v.Verify(oldToken, want, 2_000_000_000); err != nil || v.Identity() != identity {
		t.Fatal("caller mutation changed immutable verifier", err)
	}
}

func TestKeyIssuanceCutoffExpiryAndSnapshotExpiry(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	config := snapshotConfig(trust)
	v, err := accessgrant.NewVerifier(config)
	if err != nil {
		t.Fatal(err)
	}
	claims.Bindings.IssuedAtNS = 1_500_000_000
	claims.Bindings.NotBeforeNS = 1_500_000_000
	token := sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	if _, err := v.Verify(token, want, 2_000_000_000); err == nil {
		t.Fatal("issued at signing cutoff accepted")
	}
	_, _, _, claims = fixture(t)
	token = sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	config.Keys[0].VerifyUntilNS = 2_000_000_000
	v, err = accessgrant.NewVerifier(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(token, want, 1_500_000_000); err == nil {
		t.Fatal("token exceeds key validation horizon")
	}
	config = snapshotConfig(trust)
	config.ValidUntilNS = 1_500_000_000
	v, err = accessgrant.NewVerifier(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(token, want, 1_500_000_000); err == nil {
		t.Fatal("stale snapshot accepted")
	}
}

func TestInvalidSnapshotsAndEmergencyRemoval(t *testing.T) {
	priv, trust, want, claims := fixture(t)
	baseline := snapshotConfig(trust)
	for _, mutate := range []func(*accessgrant.SnapshotConfig){
		func(c *accessgrant.SnapshotConfig) { c.Version = 0 }, func(c *accessgrant.SnapshotConfig) { c.Keys = append(c.Keys, c.Keys[0]) },
		func(c *accessgrant.SnapshotConfig) { c.Keys[0].Algorithm = "HS256" }, func(c *accessgrant.SnapshotConfig) { c.Keys[0].PublicKey = []byte{1} },
		func(c *accessgrant.SnapshotConfig) { c.Keys[0].SignUntilNS = c.Keys[0].EnabledAtNS }, func(c *accessgrant.SnapshotConfig) { c.ValidUntilNS = c.ValidFromNS }} {
		c := baseline
		c.Keys = append([]accessgrant.KeyConfig(nil), baseline.Keys...)
		mutate(&c)
		if v, err := accessgrant.NewVerifier(c); err != accessgrant.ErrInvalid || v != nil {
			t.Fatal("invalid config accepted")
		}
	}
	v, err := accessgrant.NewVerifier(baseline)
	if err != nil {
		t.Fatal(err)
	}
	token := sign(t, priv, raw(t, claims), "akos-access-grant+jwt", "key-1")
	if _, err := v.Verify(token, want, 1_500_000_000); err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	replacement := baseline
	replacement.Version = 2
	replacement.Keys = []accessgrant.KeyConfig{{KeyID: "new-key", Algorithm: "EdDSA", PublicKey: pub, EnabledAtNS: 1_000_000_000, SignUntilNS: 3_000_000_000, VerifyUntilNS: 4_000_000_000}}
	fresh, err := accessgrant.NewVerifier(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Verify(token, want, 1_500_000_000); err == nil {
		t.Fatal("removed key accepted in replacement")
	}
	// Old instances remain unchanged; orchestration must actually replace them.
	if _, err := v.Verify(token, want, 1_500_000_000); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotDigestIndependentOfInputKeyOrder(t *testing.T) {
	_, trust, _, _ := fixture(t)
	c := snapshotConfig(trust)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c.Keys = append(c.Keys, accessgrant.KeyConfig{KeyID: "key-2", Algorithm: "EdDSA", PublicKey: pub, EnabledAtNS: 0, SignUntilNS: 3_000_000_000, VerifyUntilNS: 4_000_000_000})
	a, err := accessgrant.NewVerifier(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Keys[0], c.Keys[1] = c.Keys[1], c.Keys[0]
	b, err := accessgrant.NewVerifier(c)
	if err != nil || a.Identity() != b.Identity() {
		t.Fatal("key order changed snapshot identity", err)
	}
	c.Version++
	changed, err := accessgrant.NewVerifier(c)
	if err != nil || changed.Identity().Digest == a.Identity().Digest {
		t.Fatal("version not bound", err)
	}
}

package accessgrant

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
)

type KeyConfig struct {
	KeyID         string `json:"key_id"`
	Algorithm     string `json:"algorithm"`
	PublicKey     []byte `json:"public_key"`
	EnabledAtNS   int64  `json:"enabled_at_ns"`
	SignUntilNS   int64  `json:"sign_until_ns"`
	VerifyUntilNS int64  `json:"verify_until_ns"`
}

type SnapshotConfig struct {
	SnapshotID    string      `json:"snapshot_id"`
	Version       uint64      `json:"version"`
	IssuerID      string      `json:"issuer_id"`
	ValidFromNS   int64       `json:"valid_from_ns"`
	ValidUntilNS  int64       `json:"valid_until_ns"`
	MaxLifetimeNS int64       `json:"max_lifetime_ns"`
	Keys          []KeyConfig `json:"keys"`
}

// Verifier owns a copied immutable public-key snapshot. Replacing a snapshot
// constructs a new instance; it never mutates the old verifier or fetches keys.
type Verifier struct {
	id          string
	version     uint64
	digest      string
	from, until int64
	trust       Trust
	keys        map[string]KeyConfig
}

type SnapshotIdentity struct {
	ID      string
	Version uint64
	Digest  string
}

func NewVerifier(config SnapshotConfig) (*Verifier, error) {
	if config.SnapshotID == "" || !utf8.ValidString(config.SnapshotID) || len(config.SnapshotID) > 200 || config.Version == 0 || config.IssuerID == "" || !utf8.ValidString(config.IssuerID) || len(config.IssuerID) > 200 || config.ValidFromNS < 0 || config.ValidUntilNS <= config.ValidFromNS || config.MaxLifetimeNS <= 0 || len(config.Keys) == 0 || len(config.Keys) > 1000 {
		return nil, ErrInvalid
	}
	v := &Verifier{id: config.SnapshotID, version: config.Version, from: config.ValidFromNS, until: config.ValidUntilNS,
		trust: Trust{IssuerID: config.IssuerID, MaxLifetimeNS: config.MaxLifetimeNS, PublicKeys: map[string]ed25519.PublicKey{}}, keys: map[string]KeyConfig{}}
	copied := make([]KeyConfig, 0, len(config.Keys))
	for _, k := range config.Keys {
		if k.KeyID == "" || !utf8.ValidString(k.KeyID) || len(k.KeyID) > 200 || k.Algorithm != "EdDSA" || len(k.PublicKey) != ed25519.PublicKeySize || k.EnabledAtNS < 0 || k.SignUntilNS <= k.EnabledAtNS || k.VerifyUntilNS < k.SignUntilNS {
			return nil, ErrInvalid
		}
		if _, exists := v.keys[k.KeyID]; exists {
			return nil, ErrInvalid
		}
		k.PublicKey = append([]byte(nil), k.PublicKey...)
		v.keys[k.KeyID] = k
		v.trust.PublicKeys[k.KeyID] = ed25519.PublicKey(k.PublicKey)
		copied = append(copied, k)
	}
	sort.Slice(copied, func(i, j int) bool { return copied[i].KeyID < copied[j].KeyID })
	config.Keys = copied
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, ErrInvalid
	}
	encoded, err = canonical.JSON(encoded)
	if err != nil {
		return nil, ErrInvalid
	}
	h := sha256.New()
	h.Write([]byte("akos.key-snapshot/1\x00"))
	h.Write(encoded)
	v.digest = hex.EncodeToString(h.Sum(nil))
	return v, nil
}

func (v *Verifier) Identity() SnapshotIdentity {
	if v == nil {
		return SnapshotIdentity{}
	}
	return SnapshotIdentity{v.id, v.version, v.digest}
}

// Verify checks snapshot/key windows in addition to the SDK signature and
// binding. It still does not authorize current resource access or replay.
func (v *Verifier) Verify(token string, expected Expectation, nowNS int64) (Claims, error) {
	if v == nil || nowNS < v.from || nowNS >= v.until {
		return Claims{}, ErrInvalid
	}
	claims, err := Verify(token, v.trust, expected, nowNS)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	// Obtain the protected kid without exposing a caller-controlled key object.
	header, err := protectedKeyID(token)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	k, ok := v.keys[header]
	b := claims.Bindings
	if !ok || nowNS < k.EnabledAtNS || nowNS >= k.VerifyUntilNS || b.IssuedAtNS < k.EnabledAtNS || b.IssuedAtNS >= k.SignUntilNS || b.ExpiresAtNS > k.VerifyUntilNS {
		return Claims{}, ErrInvalid
	}
	return claims, nil
}

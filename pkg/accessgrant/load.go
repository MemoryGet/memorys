package accessgrant

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
)

// LoadVerifier accepts only the closed JSON public-key snapshot and matches an
// independently obtained deployment identity. It does not authenticate that
// identity's origin or consult the durable high-water version.
func LoadVerifier(raw []byte, expected SnapshotIdentity) (*Verifier, error) {
	if expected.ID == "" || expected.Version == 0 || !digest(expected.Digest) {
		return nil, ErrInvalid
	}
	var config SnapshotConfig
	if strict(raw, &config) != nil {
		return nil, ErrInvalid
	}
	outer, ok := fields(raw, "snapshot_id", "version", "issuer_id", "valid_from_ns", "valid_until_ns", "max_lifetime_ns", "keys")
	if !ok {
		return nil, ErrInvalid
	}
	for _, name := range []string{"version", "valid_from_ns", "valid_until_ns", "max_lifetime_ns"} {
		if !integerPattern.Match(bytes.TrimSpace(outer[name])) {
			return nil, ErrInvalid
		}
	}
	var keys []json.RawMessage
	if json.Unmarshal(outer["keys"], &keys) != nil || len(keys) == 0 || len(keys) > 1000 {
		return nil, ErrInvalid
	}
	for _, rawKey := range keys {
		key, ok := fields(rawKey, "key_id", "algorithm", "public_key", "enabled_at_ns", "sign_until_ns", "verify_until_ns")
		if !ok {
			return nil, ErrInvalid
		}
		for _, name := range []string{"enabled_at_ns", "sign_until_ns", "verify_until_ns"} {
			if !integerPattern.Match(bytes.TrimSpace(key[name])) {
				return nil, ErrInvalid
			}
		}
		var value string
		if json.Unmarshal(key["public_key"], &value) != nil {
			return nil, ErrInvalid
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil || base64.StdEncoding.EncodeToString(decoded) != value {
			return nil, ErrInvalid
		}
	}
	v, err := NewVerifier(config)
	if err != nil || v.Identity() != expected {
		return nil, ErrInvalid
	}
	return v, nil
}

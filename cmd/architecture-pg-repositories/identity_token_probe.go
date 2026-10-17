package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	"github.com/MemoryGet/memorys/pkg/postgres"
	jose "github.com/go-jose/go-jose/v4"
)

var identityTokenSamples []map[string]any

// Probe-only credential; its owner clears the bytes before returning.
var historicalSecondCredential contracts.Credential

func nativeIdentityAuthenticator(ctx context.Context, db *sql.DB) (*identitytoken.Authenticator, contracts.Credential, error) {
	var empty contracts.Credential
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, empty, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, empty, err
	}
	fingerprint := sha256.Sum256(pub)
	digest := hex.EncodeToString(fingerprint[:])
	now := time.Now().Unix()
	issued, expires := now-1, now+120
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO akos_withdraw_v1.identity_issuers VALUES('fixture',true)`, nil},
		{`INSERT INTO akos_withdraw_v1.identity_signing_keys VALUES('fixture','identity-key',$1,true,$2,$3)`, []any{digest, now - 10, now + 300}},
		{`INSERT INTO akos_withdraw_v1.identity_subjects VALUES('fixture','reader','user',true)`, nil},
		{`INSERT INTO akos_withdraw_v1.identity_clients VALUES('fixture','identity-client',true)`, nil},
		{`INSERT INTO akos_withdraw_v1.identity_tokens VALUES('fixture','identity-token','identity-key','reader','identity-client',$1,$2,false)`, []any{issued, expires}},
	}
	for _, s := range statements {
		if _, err := db.ExecContext(ctx, s.sql, s.args...); err != nil {
			return nil, empty, err
		}
	}
	claims := map[string]any{"iss": "fixture", "sub": "reader", "client_id": "identity-client", "aud": "knowledge-api", "jti": "identity-token", "iat": issued, "nbf": issued, "exp": expires}
	sign := func() (contracts.Credential, error) {
		opts := (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "identity-key")
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, opts)
		if err != nil {
			return empty, err
		}
		raw, _ := json.Marshal(claims)
		signed, err := signer.Sign(raw)
		if err != nil {
			return empty, err
		}
		token, err := signed.CompactSerialize()
		return contracts.Credential{Kind: "access_token", Secret: []byte(token)}, err
	}
	credential, err := sign()
	if err != nil {
		return nil, empty, err
	}
	config := identitytoken.Config{Issuer: "fixture", Audience: "knowledge-api", SubjectType: "user", MaxLifetime: 5 * time.Minute, Keys: map[string]*rsa.PublicKey{"identity-key": &key.PublicKey}}
	auth, err := identitytoken.New(config, postgres.IdentityStatus{DB: db})
	if err != nil {
		return nil, empty, err
	}
	who, err := auth.Authenticate(ctx, credential)
	if err != nil || who.SubjectID != "reader" || who.ClientID != "identity-client" {
		return nil, empty, errors.New("native SDK identity verification failed")
	}
	for _, change := range []struct{ disable, restore string }{
		{`UPDATE akos_withdraw_v1.identity_tokens SET revoked=true`, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false`},
		{`UPDATE akos_withdraw_v1.identity_subjects SET enabled=false`, `UPDATE akos_withdraw_v1.identity_subjects SET enabled=true`},
		{`UPDATE akos_withdraw_v1.identity_clients SET enabled=false`, `UPDATE akos_withdraw_v1.identity_clients SET enabled=true`},
		{`UPDATE akos_withdraw_v1.identity_signing_keys SET enabled=false`, `UPDATE akos_withdraw_v1.identity_signing_keys SET enabled=true`},
	} {
		if _, err := db.ExecContext(ctx, change.disable); err != nil {
			return nil, empty, err
		}
		if _, err := auth.Authenticate(ctx, credential); err == nil {
			return nil, empty, errors.New("native revoked/disabled identity accepted")
		}
		if _, err := db.ExecContext(ctx, change.restore); err != nil {
			return nil, empty, err
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_signing_keys SET public_key_digest=$1`, strings.Repeat("0", 64)); err != nil {
		return nil, empty, err
	}
	if _, err := auth.Authenticate(ctx, credential); err == nil {
		return nil, empty, errors.New("rebound public key accepted")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_signing_keys SET public_key_digest=$1`, digest); err != nil {
		return nil, empty, err
	}
	claims["jti"] = "unknown-token"
	unknown, err := sign()
	if err != nil {
		return nil, empty, err
	}
	if _, err := auth.Authenticate(ctx, unknown); err == nil {
		return nil, empty, errors.New("unregistered signed token accepted")
	}
	clear(unknown.Secret)
	claims["jti"] = "identity-token"
	broken, err := identitytoken.New(config, postgres.IdentityStatus{})
	if err != nil {
		return nil, empty, err
	}
	if _, err := broken.Authenticate(ctx, credential); err == nil {
		return nil, empty, errors.New("unavailable status source accepted")
	}
	if _, err := auth.Authenticate(ctx, credential); err != nil {
		return nil, empty, err
	}
	tokenHash := sha256.Sum256(credential.Secret)
	identityTokenSamples = append(identityTokenSamples, map[string]any{"principal": who, "claims": claims, "public_key_digest": digest, "token_sha256": hex.EncodeToString(tokenHash[:]), "algorithm": "RS256", "header_type": "at+jwt", "status_source": "postgres-primary-allowlist", "issuer_keys": "ephemeral-test-fixture"})
	// Issue a separately registered reader with the same trusted test key.
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.identity_subjects VALUES('fixture','historical-second-reader','user',true)`); err != nil {
		return nil, empty, err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.identity_tokens VALUES('fixture','historical-second-token','identity-key','historical-second-reader','identity-client',$1,$2,false)`, issued, expires); err != nil {
		return nil, empty, err
	}
	// Do not mutate the claims map already captured by identityTokenSamples.
	secondClaims := make(map[string]any, len(claims))
	for k, v := range claims {
		secondClaims[k] = v
	}
	secondClaims["sub"], secondClaims["jti"] = "historical-second-reader", "historical-second-token"
	claims = secondClaims
	historicalSecondCredential, err = sign()
	if err != nil {
		return nil, empty, err
	}
	return auth, credential, nil
}

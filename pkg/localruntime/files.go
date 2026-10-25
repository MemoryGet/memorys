package localruntime

import (
	"database/sql"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// NewSearchFromFiles is shared by the explicit local CLI and environment-based
// server. Neither host needs a signing private key to serve HTTP searches.
func NewSearchFromFiles(db *sql.DB, issuer, audience, subjectType, jwksFile, profileFile string, lifetime time.Duration) (*Search, error) {
	keys, err := identitytoken.LoadRS256PublicKeys(jwksFile)
	if err != nil {
		return nil, errors.New("search JWKS rejected")
	}
	profile, err := configprofile.LoadFile(profileFile)
	if err != nil {
		return nil, errors.New("search profile rejected")
	}
	auth, err := identitytoken.New(identitytoken.Config{Issuer: issuer, Audience: audience, SubjectType: subjectType, Keys: keys, MaxLifetime: lifetime}, postgres.IdentityStatus{DB: db})
	if err != nil {
		return nil, errors.New("search identity configuration rejected")
	}
	return NewSearch(db, auth, profile)
}

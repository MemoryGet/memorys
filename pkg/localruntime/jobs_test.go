package localruntime

import (
	"database/sql"
	"testing"

	"github.com/MemoryGet/memorys/pkg/configprofile"
)

func TestNewJobApplicationsRejectsNilRuntimeDependencies(t *testing.T) {
	var nilRuntime *Search
	if _, err := nilRuntime.NewJobApplications(); err == nil {
		t.Fatal("nil runtime accepted")
	}
	if _, err := (&Search{db: &sql.DB{}, auth: nil, profile: mustProfileBytes(t, testProfile(t))}).NewJobApplications(); err == nil {
		t.Fatal("runtime without authenticator accepted")
	}
}

func mustProfileBytes(t *testing.T, profile configprofile.Snapshot) []byte {
	t.Helper()
	raw, err := configprofile.Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

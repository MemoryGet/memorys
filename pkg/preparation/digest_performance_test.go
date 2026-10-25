package preparation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

// Frozen pre-optimization encoding, independent of the typed digest envelope.
func referenceArtifactDigest(a preparation.Artifact) contracts.Digest {
	raw, _ := json.Marshal(a)
	var body map[string]json.RawMessage
	json.Unmarshal(raw, &body)
	delete(body, "Digest")
	raw, _ = json.Marshal(map[string]any{"format": "akos.prepared-artifact/1", "artifact": body})
	raw, _ = canonical.JSON(raw)
	sum := sha256.Sum256(append([]byte("akos.prepared-artifact/1\x00"), raw...))
	return contracts.Digest(hex.EncodeToString(sum[:]))
}

func TestArtifactDigestEncodingCompatibilityAndAllocation(t *testing.T) {
	w, p, chunks := fixture(t)
	a, _, err := preparation.Build(w, p, chunks)
	if err != nil {
		t.Fatal(err)
	}
	// Include all top-level fields, map ordering, exact uint64 and escaping.
	for n := 0; n < 40; n++ {
		a.OperationID = contracts.ID(fmt.Sprintf("operation-%d-中文😀<>&", n))
		a.JobID = contracts.ID(fmt.Sprintf("job-%d", n))
		a.AttemptID = contracts.ID(fmt.Sprintf("attempt-%d", n))
		a.FencingToken = ^uint64(0) - uint64(n)
		a.SnapshotDigest = contracts.Digest(strings.Repeat(fmt.Sprintf("%x", n%16), 64))
		a.Digest = contracts.Digest(strings.Repeat("f", 64))
		a.Profile.Version = uint64(n + 1)
		want := referenceArtifactDigest(a)
		got, err := preparation.ArtifactDigest(a)
		if err != nil || got != want {
			t.Fatalf("digest bytes changed: %s != %s: %v", got, want, err)
		}
	}
	old := testing.AllocsPerRun(10, func() { referenceArtifactDigest(a) })
	new := testing.AllocsPerRun(10, func() { preparation.ArtifactDigest(a) })
	t.Logf("artifact digest allocations: reference=%.0f current=%.0f", old, new)
	if new > old*.8 {
		t.Fatalf("artifact digest allocation budget exceeded: %.0f > 80%% of %.0f", new, old)
	}
}

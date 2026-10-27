package preparation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestArtifactDigestMemoRemainsBoundedAndFallsBack(t *testing.T) {
	var memo ArtifactDigestMemo
	var artifact Artifact
	for n := 0; n < 1100; n++ {
		artifact.OperationID = contracts.ID(fmt.Sprintf("operation-%d", n))
		got, err := memo.Digest(artifact)
		want, originalErr := ArtifactDigest(artifact)
		if err != nil || originalErr != nil || got != want {
			t.Fatalf("uncached digest differs: %s != %s: %v %v", got, want, err, originalErr)
		}
	}
	if len(memo.entries) > 1000 || memo.bytes > 4<<20 {
		t.Fatalf("memo exceeded its bounds: %d entries, %d bytes", len(memo.entries), memo.bytes)
	}
	var large ArtifactDigestMemo
	for n := 0; n < 300; n++ {
		artifact.OperationID = contracts.ID(fmt.Sprintf("%04d", n) + strings.Repeat("x", 16<<10))
		if _, err := large.Digest(artifact); err != nil {
			t.Fatal(err)
		}
	}
	if large.bytes > 4<<20 || len(large.entries) >= 300 {
		t.Fatalf("large memo exceeded its byte bound: %d entries, %d bytes", len(large.entries), large.bytes)
	}
	got, err := large.Digest(artifact)
	want, originalErr := ArtifactDigest(artifact)
	if err != nil || originalErr != nil || got != want {
		t.Fatalf("digest after budget exhaustion differs: %s != %s: %v %v", got, want, err, originalErr)
	}
}

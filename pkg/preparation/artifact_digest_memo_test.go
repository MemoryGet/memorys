package preparation_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

func TestArtifactDigestMemoPreservesDigestAndRejectsInvalidInput(t *testing.T) {
	w, profile, chunks := fixture(t)
	artifact, _, err := preparation.Build(w, profile, chunks)
	if err != nil {
		t.Fatal(err)
	}
	var memo preparation.ArtifactDigestMemo
	for n := 0; n < 40; n++ {
		artifact.OperationID = contracts.ID(fmt.Sprintf("operation-%d-中文😀", n))
		artifact.FencingToken = ^uint64(0) - uint64(n)
		want := referenceArtifactDigest(artifact)
		for attempt := 0; attempt < 2; attempt++ {
			got, err := memo.Digest(artifact)
			if err != nil || got != want {
				t.Fatalf("digest mismatch for changed artifact: %s != %s: %v", got, want, err)
			}
		}
	}
	// Digest is excluded from the hash, but must still satisfy UTF-8 validation.
	artifact.Digest = contracts.Digest(string([]byte{'x', 0xff}))
	if _, err := memo.Digest(artifact); err == nil {
		t.Fatal("invalid UTF-8 in the excluded digest was accepted")
	}
}

func TestArtifactDigestMemoConcurrentCalls(t *testing.T) {
	w, profile, chunks := fixture(t)
	artifact, _, err := preparation.Build(w, profile, chunks)
	if err != nil {
		t.Fatal(err)
	}
	want, err := preparation.ArtifactDigest(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var memo preparation.ArtifactDigestMemo
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if got, err := memo.Digest(artifact); err != nil || got != want {
					t.Errorf("concurrent digest mismatch: %s != %s: %v", got, want, err)
				}
			}
		}()
	}
	wg.Wait()
}

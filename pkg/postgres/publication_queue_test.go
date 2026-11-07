package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/preparation"
)

func TestPublicationQueueCommandIDIsStableForSameJobVersion(t *testing.T) {
	one := publicationQueueCommandID("claim", "job", "worker", 3)
	two := publicationQueueCommandID("claim", "job", "worker", 3)
	if one == "" || one != two || one == publicationQueueCommandID("claim", "job", "worker", 4) || one == publicationQueueCommandID("request", "job", "worker", 3) {
		t.Fatal("queue command identity is not bound to operation and version")
	}
}

func TestPublicationQueueRejectsUnconfiguredDiscovery(t *testing.T) {
	_, err := (PublicationQueue{}).Next(context.Background())
	if !errors.Is(err, ErrPublicationQueue) || errors.Is(err, preparation.ErrRunnerNoWork) {
		t.Fatalf("invalid queue configuration was accepted: %v", err)
	}
	q := PublicationQueue{Space: "space", KnowledgeBase: "kb", WorkerID: "worker", ProfileDigest: "bad", LeaseDuration: time.Second}
	if _, err := q.Next(context.Background()); !errors.Is(err, ErrPublicationQueue) {
		t.Fatalf("invalid profile was accepted: %v", err)
	}
}

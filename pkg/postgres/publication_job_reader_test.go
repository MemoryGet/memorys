package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestPublicationTaskResourceReaderRejectsUnconfiguredAccess(t *testing.T) {
	reader := PublicationTaskResourceReader{}
	if _, err := reader.ReadResourceJob(context.Background(), contracts.JobReadRequest{JobID: "job"}); !errors.Is(err, contracts.ErrJobReadDenied) {
		t.Fatalf("unconfigured reader returned %v", err)
	}
}

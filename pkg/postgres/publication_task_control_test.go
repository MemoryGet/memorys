package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestPublicationTaskControlRejectsInvalidInput(t *testing.T) {
	control := PublicationTaskControl{}
	if _, err := control.CancelPublicationTask(context.Background(), contracts.PublicationTaskControlRequest{JobID: "job"}); !errors.Is(err, ErrPublicationTaskControl) {
		t.Fatalf("invalid control accepted: %v", err)
	}
}

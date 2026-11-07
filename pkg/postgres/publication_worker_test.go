package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/preparation"
)

func TestNewPublicationServiceRejectsUnwiredRunner(t *testing.T) {
	_, err := NewPublicationService(preparation.Runner{}, PublicationQueue{}, time.Second, nil)
	if !errors.Is(err, preparation.ErrWorkerService) {
		t.Fatal("unwired publication service accepted", err)
	}
}

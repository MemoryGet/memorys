package moduletransport_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/moduletransport"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

type brokenCloseStage struct {
	bytes.Buffer
	aborted bool
}

func (stage *brokenCloseStage) Close() error { return errors.New("SECRET flush failed") }
func (stage *brokenCloseStage) Abort(ctx context.Context) error {
	stage.aborted = true
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("SECRET cleanup failed")
}

func TestUploadCloseAndCleanupFailureRemainUnconfirmed(t *testing.T) {
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	stage := &brokenCloseStage{}
	receipt, err := moduletransport.ReceiveBlobMultipart(bounded(t), uploadRequest(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}}), uploadLimits(), allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
		return stage, nil
	})
	if !errors.Is(err, moduletransport.ErrStaging) || !errors.Is(err, moduletransport.ErrCleanup) || !stage.aborted || receipt.Call.InvocationID != "" || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("failed flush/cleanup became confirmation", err, receipt)
	}
}

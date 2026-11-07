package publicationassembly

import (
	"context"
	"testing"

	"github.com/MemoryGet/memorys/pkg/localruntime"
)

func TestBuildIsOptInAndRequiresWorkerConfiguration(t *testing.T) {
	t.Setenv("MEMORYS_PUBLICATION_ENABLED", "0")
	service, err := Build(context.Background(), nil)
	if err != nil || service != nil {
		t.Fatalf("disabled publication assembly returned service=%v err=%v", service, err)
	}
	t.Setenv("MEMORYS_PUBLICATION_ENABLED", "1")
	service, err = Build(context.Background(), &localruntime.Search{})
	if err == nil || service != nil {
		t.Fatalf("missing publication configuration accepted service=%v err=%v", service, err)
	}
}

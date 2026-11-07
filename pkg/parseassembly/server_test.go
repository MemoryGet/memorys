package parseassembly

import (
	"context"
	"testing"

	"github.com/MemoryGet/memorys/pkg/localruntime"
)

func TestBuildIsOptInAndRequiresPartitionConfiguration(t *testing.T) {
	t.Setenv("MEMORYS_PARSE_ENABLED", "0")
	runtime, err := Build(context.Background(), nil)
	if err != nil || runtime != nil {
		t.Fatalf("disabled parser assembly returned runtime=%v err=%v", runtime, err)
	}
	t.Setenv("MEMORYS_PARSE_ENABLED", "1")
	runtime, err = Build(context.Background(), &localruntime.Search{})
	if err == nil || runtime != nil {
		t.Fatalf("missing parser configuration accepted runtime=%v err=%v", runtime, err)
	}
}

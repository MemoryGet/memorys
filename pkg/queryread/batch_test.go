package queryread_test

import (
	"context"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type batchCurrentRepository struct {
	currentRepository
	calls  int
	change func([]contracts.CurrentDocumentState) []contracts.CurrentDocumentState
}

func (r *batchCurrentRepository) LoadCurrentDocuments(c context.Context, ids []contracts.ID) ([]contracts.CurrentDocumentState, error) {
	r.calls++
	out := []contracts.CurrentDocumentState{}
	for _, id := range ids {
		s, e := r.currentRepository.LoadCurrentDocument(c, id)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	if r.change != nil {
		out = r.change(out)
	}
	return out, nil
}

func TestBatchReadGuardValidatesWholeResultAndCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	id := contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "view", Watermark: "watermark"}
	for name, change := range map[string]func([]contracts.CurrentDocumentState) []contracts.CurrentDocumentState{
		"missing":   func(s []contracts.CurrentDocumentState) []contracts.CurrentDocumentState { return s[:1] },
		"duplicate": func(s []contracts.CurrentDocumentState) []contracts.CurrentDocumentState { s[1] = s[0]; return s },
		"foreign": func(s []contracts.CurrentDocumentState) []contracts.CurrentDocumentState {
			s[1].Document.SpaceID = "other"
			return s
		},
		"swapped": func(s []contracts.CurrentDocumentState) []contracts.CurrentDocumentState {
			s[0], s[1] = s[1], s[0]
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := &batchCurrentRepository{change: change}
			g, e := queryread.NewGuard(ctx, r, currentScope(), id)
			if e != nil {
				t.Fatal(e)
			}
			defer g.Close()
			got, e := queryread.LoadCurrentDocuments(ctx, g, []contracts.ID{"one", "two"})
			if e == nil || len(got) != 0 || r.calls != 1 {
				t.Fatal("invalid batch escaped", got, e, r.calls)
			}
		})
	}
	r := &batchCurrentRepository{}
	g, e := queryread.NewGuard(ctx, r, currentScope(), id)
	if e != nil {
		t.Fatal(e)
	}
	for _, ids := range [][]contracts.ID{{"one", "one"}, {""}, {}} {
		if _, e := queryread.LoadCurrentDocuments(ctx, g, ids); e == nil || r.calls != 0 {
			t.Fatal("invalid input reached batch provider")
		}
	}
	if got, e := queryread.LoadCurrentDocuments(ctx, g, []contracts.ID{"one", "two"}); e != nil || len(got) != 2 || r.calls != 1 {
		t.Fatal(got, e)
	}
	g.Close()
	if _, e := queryread.LoadCurrentDocuments(ctx, g, []contracts.ID{"one"}); e == nil || r.calls != 1 {
		t.Fatal("batch handle escaped close")
	}
}

package queryread_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func checkEvidence() contracts.Evidence {
	return contracts.Evidence{Rank: 1, SourceExcerpt: "中文知识😀", Citation: contracts.Citation{DocumentID: "doc", RevisionID: "revision", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "exact", Characters: &contracts.CharacterRange{Start: 0, End: 6}}, DerivedFrom: []contracts.OriginalAnchor{{BlobID: "original", Locator: contracts.Locator{Precision: "coarse"}}}}}}
}

func TestCurrentCheckForwardsFrozenEvidenceWithoutProviderMutation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var log []string
	op, c, r, b := checkFixture(&log)
	op.Evidence = []contracts.Evidence{checkEvidence()}
	wanted := []contracts.Evidence{checkEvidence()}
	verified := false
	b.inspect = func(got []contracts.Evidence) {
		verified = true
		if !reflect.DeepEqual(got, wanted) {
			t.Fatal("backend received different evidence")
		}
	}
	if err := queryread.CheckCurrent(ctx, op, c, r, b, checkLimits); err != nil {
		t.Fatal(err)
	}
	if !verified || !reflect.DeepEqual(op.Evidence, wanted) {
		t.Fatal("missing verification or mutated original evidence")
	}
}

func TestCurrentCheckRejectsForeignEvidenceBeforeProviderCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	changes := map[string]func(*queryread.CheckOperation){
		"foreign document":     func(o *queryread.CheckOperation) { o.Evidence[0].Citation.DocumentID = "other" },
		"foreign revision":     func(o *queryread.CheckOperation) { o.Evidence[0].Citation.RevisionID = "other" },
		"missing block":        func(o *queryread.CheckOperation) { o.Evidence[0].Citation.BlockID = "" },
		"wrong revision block": func(o *queryread.CheckOperation) { o.Evidence[0].Citation.Source.RevisionBlockID = "other" },
		"mixed anchor":         func(o *queryread.CheckOperation) { o.Evidence[0].Citation.Source.BlobID = "unexpected" },
		"unknown anchor":       func(o *queryread.CheckOperation) { o.Evidence[0].Citation.Source.Kind = "vendor" },
		"missing original": func(o *queryread.CheckOperation) {
			o.Evidence[0].Citation.Source.Kind = "original"
			o.Evidence[0].Citation.Source.RevisionBlockID = ""
		},
		"missing derived origin":   func(o *queryread.CheckOperation) { o.Evidence[0].Citation.Source.DerivedFrom[0].BlobID = "" },
		"empty excerpt":            func(o *queryread.CheckOperation) { o.Evidence[0].SourceExcerpt = "" },
		"invalid excerpt encoding": func(o *queryread.CheckOperation) { o.Evidence[0].SourceExcerpt = string([]byte{0xff}) },
		"zero rank":                func(o *queryread.CheckOperation) { o.Evidence[0].Rank = 0 },
		"no prepared facts":        func(o *queryread.CheckOperation) { o.Expectation.Published = nil },
		"duplicate prepared document": func(o *queryread.CheckOperation) {
			o.Expectation.Published = append(o.Expectation.Published, o.Expectation.Published[0])
		},
		"too many evidence": func(o *queryread.CheckOperation) {
			e := o.Evidence[0]
			o.Evidence = make([]contracts.Evidence, 1001)
			for i := range o.Evidence {
				o.Evidence[i] = e
			}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			var log []string
			op, c, r, b := checkFixture(&log)
			op.Evidence = []contracts.Evidence{checkEvidence()}
			change(&op)
			if queryread.CheckCurrent(ctx, op, c, r, b, checkLimits) != queryread.ErrInvalid || len(log) != 0 {
				t.Fatal("unbound evidence reached providers", log)
			}
		})
	}
}

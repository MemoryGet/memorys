package queryread_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func TestSinglePublishedFactRequiresEffectiveBoundDocument(t *testing.T) {
	for _, mode := range []string{"ok", "withdraw", "deleted", "foreign-kb", "revoked", "unready", "no-approval", "wrong-revision"} {
		t.Run(mode, func(t *testing.T) {
			expected, observed := currentReviewFixture()
			state := observed.Documents[0]
			switch mode {
			case "withdraw":
				state.Document.ActiveRevisionID = nil
			case "deleted":
				now := time.Now()
				state.Document.DeletedAt = &now
			case "foreign-kb":
				state.Document.KnowledgeBaseID = "other"
			case "revoked":
				state.PublicationReview.Validity.State = "revoked"
			case "unready":
				state.GenerationSet.State = "building"
			case "no-approval":
				state.PublicationReview = nil
			case "wrong-revision":
				state.PublicationReview.Revision.ID = "other"
			}
			fact, err := queryread.PublishedCurrentFact(currentScope(), observed.Identity, state)
			if mode != "ok" {
				if err == nil || fact.DocumentID != "" {
					t.Fatal("invalid current fact admitted")
				}
				return
			}
			if err != nil || !reflect.DeepEqual(fact, expected.Published[0]) {
				t.Fatal(fact, err)
			}
			for key, g := range fact.GenerationSet.Channels {
				g.ID = "changed"
				fact.GenerationSet.Channels[key] = g
			}
			if reflect.DeepEqual(fact.GenerationSet.Channels, state.GenerationSet.Channels) {
				t.Fatal("fact aliases observed generation map")
			}
		})
	}
}

var _ contracts.ReadCursorCodec = (*queryread.HMACReadCursor)(nil)

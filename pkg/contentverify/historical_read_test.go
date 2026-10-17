package contentverify_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type historicalReader func(context.Context, contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error)

func (f historicalReader) ReadHistoricalContent(c context.Context, q contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error) {
	return f(c, q)
}

func TestHistoricalOriginalVerificationRetainsGrantOnEveryReread(t *testing.T) {
	for _, mode := range []string{"ok", "publication-changed", "projection-revoked", "snapshot-map-mutation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			base, f, r, store, _, control := approvedOriginalFixture(t, ctx, "text/plain")
			base.Scope.Action = "knowledge.read"
			history := base.Scope
			history.Action = contracts.HistoricalReadAction
			history.ScopeID = "independent-history"
			q := contracts.HistoricalContentReadRequest{Base: base, HistoryScope: history, Principal: contracts.Principal{SubjectID: base.Scope.SubjectID, SubjectType: "user", Issuer: "fixture", ClientID: "client"}}
			record := contracts.HistoricalContentRecord{Content: r.record, Review: contracts.CurrentPublicationReview{Revision: f}, PublicationID: "old-publication", PublicationEpoch: 1, StoreEpochID: "store"}
			record.Current.GenerationSet.Channels = map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: {ID: "fixed-generation"}}
			calls, projections := 0, 0
			revoked := false
			reader := historicalReader(func(_ context.Context, input contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error) {
				calls++
				if input.Base.Scope.Action != "knowledge.read" || input.HistoryScope.Action != contracts.HistoricalReadAction || input.HistoryScope.ScopeID != "independent-history" || input.Base.RevisionID != f.ID {
					t.Fatal("history binding dropped during source verification")
				}
				if revoked {
					return contracts.HistoricalContentRecord{}, errors.New("revoked")
				}
				if mode == "snapshot-map-mutation" {
					record.Current.GenerationSet.Channels[contracts.KeywordChannel] = contracts.IndexGeneration{ID: "mutated-generation"}
				}
				out := record
				if mode == "publication-changed" && calls > 1 {
					out.PublicationID = "replacement-publication"
				}
				input.HistoryScope.KnowledgeBaseIDs[0] = "provider-mutation"
				return out, nil
			})
			projector := originalProjector(func(c context.Context, m string, b []byte) (contentverify.OriginalTextProjection, error) {
				projections++
				if mode == "projection-revoked" {
					revoked = true
				}
				return (contentverify.UTF8OriginalText{}).ProjectOriginalText(c, m, b)
			})
			err := (contentverify.TextHistoricalReadVerifier{Reader: reader, Originals: store, Control: control, Projector: projector}).VerifyHistoricalContent(ctx, q, record)
			if mode == "ok" && (err != nil || projections != 1 || calls < 4) {
				t.Fatal("actual historical original verification failed", err, calls)
			}
			if mode != "ok" && err == nil {
				t.Fatal("stale historical source passed")
			}
			if q.HistoryScope.KnowledgeBaseIDs[0] != "kb" {
				t.Fatal("history grant scope aliased")
			}
		})
	}
}

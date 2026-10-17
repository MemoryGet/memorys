package knowledge_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type historicalRead func(context.Context, contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error)

func (f historicalRead) ReadHistoricalContent(c context.Context, q contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error) {
	return f(c, q)
}

func TestHistoricalReadUsesIndependentGrantAndCurrentControl(t *testing.T) {
	for _, mode := range []string{"ok", "deny-history", "wrong-action", "wrong-kb", "stale-policy", "never-published", "revoked-review", "foreign-revision", "wrong-current", "corrupt", "source-failure", "signing-revocation", "signing-review-revoked", "final-history-revoked", "mutate-input"} {
		t.Run(mode, func(t *testing.T) {
			p := newPagedFixture(t)
			p.repo.state.Document.PublicationEpoch = 2
			oldDraft := contracts.Draft{ID: "old-draft", DocumentID: "doc", State: "draft", RowVersion: 1}
			for i, id := range []contracts.ID{"old-one", "old-two"} {
				oldDraft.Blocks = append(oldDraft.Blocks, contracts.DraftBlock{ID: id, DraftID: oldDraft.ID, Order: i, RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "历史正文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: id, Locator: contracts.Locator{Precision: "coarse"}}}})
			}
			old, err := document.BuildFrozenContent(oldDraft)
			if err != nil {
				t.Fatal(err)
			}
			current, err := queryread.PublishedCurrentFact(p.scope, contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "view", Watermark: "head"}, p.repo.state)
			if err != nil {
				t.Fatal(err)
			}
			record := contracts.HistoricalContentRecord{Content: contracts.FrozenContentRecord{DocumentID: "doc", RevisionID: "old-rev", Payload: old.Payload}, PublicationID: "old-publication", PublicationEpoch: 1, StoreEpochID: "store", Current: current, Review: contracts.CurrentPublicationReview{Revision: contracts.FrozenRevisionFact{ID: "old-rev", DocumentID: "doc", ContentHash: old.ContentHash, BlocksHash: old.BlocksHash, HashFormat: old.HashFormat}, Approval: contracts.Approval{ID: "old-approval", RevisionID: "old-rev", ContentHash: old.ContentHash, PolicyVersion: 1, Decision: "approve", ActorID: "reviewer", RowVersion: 1, DecidedAt: time.Now()}, Validity: contracts.ApprovalValidityFact{ApprovalID: "old-approval", ApprovalRowVersion: 1, ReviewPolicyVersion: 1, State: "effective"}}}
			historyCalls, grantCalls := 0, 0
			denied := false
			authorizer := authorize(func(_ context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
				grantCalls++
				if q.Action != contracts.HistoricalReadAction || !reflect.DeepEqual(q.ResourceIDs, []contracts.ID{"doc"}) || q.Principal.SubjectID != "reader" {
					t.Fatal("history authority binding lost")
				}
				if mode == "deny-history" || denied || mode == "final-history-revoked" && grantCalls > 1 {
					return contracts.AccessScope{}, errors.New("private grant rejection")
				}
				s := p.scope
				s.Action = contracts.HistoricalReadAction
				s.ScopeID = "history-scope"
				s.KnowledgeBaseIDs = append([]contracts.ID(nil), s.KnowledgeBaseIDs...)
				switch mode {
				case "wrong-action":
					s.Action = "knowledge.read"
				case "wrong-kb":
					s.KnowledgeBaseIDs[0] = "other"
				case "stale-policy":
					s.PolicyVersion++
				}
				return s, nil
			})
			reader := historicalRead(func(_ context.Context, q contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error) {
				historyCalls++
				if q.Base.Scope.Action != "knowledge.read" || q.HistoryScope.Action != contracts.HistoricalReadAction || q.Base.RevisionID != "old-rev" || q.HistoryScope.KnowledgeBaseIDs[0] != "kb" {
					t.Fatal("history source lost independent grant")
				}
				if mode == "source-failure" {
					return contracts.HistoricalContentRecord{}, errors.New("private source error")
				}
				out := record
				switch mode {
				case "never-published":
					out.PublicationID = ""
				case "revoked-review":
					out.Review.Validity.State = "revoked"
				case "foreign-revision":
					out.Content.RevisionID = "foreign"
				case "wrong-current":
					out.Current.PublicationEpoch++
				case "corrupt":
					out.Content.Payload = []byte(`{}`)
				case "mutate-input":
					q.HistoryScope.KnowledgeBaseIDs[0] = "attacker"
					q.Base.Scope.KnowledgeBaseIDs[0] = "attacker"
				}
				return out, nil
			})
			d := p.dependencies
			d.History = &knowledge.HistoryReadDependencies{Authorizer: authorizer, Content: reader, Verifier: contentverify.TextHistoricalReadVerifier{Reader: reader}}
			p.service, err = knowledge.NewCurrentRead(d)
			if err != nil {
				t.Fatal(err)
			}
			p.input.RevisionID = "old-rev"
			if mode == "signing-revocation" {
				p.onSeal = func() { denied = true }
			}
			if mode == "signing-review-revoked" {
				p.onSeal = func() { record.Review.Validity.State = "revoked" }
			}
			page, err := p.service.ReadPage(context.Background(), p.input, knowledge.ReadPageOptions{})
			good := mode == "ok" || mode == "mutate-input"
			if good {
				if err != nil || page.Record.RevisionID != "old-rev" || !reflect.DeepEqual(page.BlockIDs, []contracts.ID{"old-one"}) || page.NextCursor == "" || grantCalls != 2 || p.authCalls != 2 || historyCalls < 3 {
					t.Fatal("historical read/paging failed", err, historyCalls, grantCalls)
				}
				last, e := p.service.ReadPage(context.Background(), p.input, knowledge.ReadPageOptions{Cursor: page.NextCursor})
				if e != nil || !reflect.DeepEqual(last.BlockIDs, []contracts.ID{"old-two"}) || last.NextCursor != "" {
					t.Fatal("historical continuation failed", e)
				}
			} else if err == nil || len(page.Record.Payload) != 0 || page.NextCursor != "" {
				t.Fatal("unauthorized/stale historical content escaped")
			}
			if (mode == "deny-history" || mode == "wrong-action" || mode == "wrong-kb" || mode == "stale-policy") && historyCalls != 0 {
				t.Fatal("invalid history grant reached source")
			}
			if p.scope.KnowledgeBaseIDs[0] != "kb" {
				t.Fatal("caller scope mutated")
			}
		})
	}
}

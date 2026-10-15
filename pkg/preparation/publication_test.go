package preparation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

func publicationFixture(t *testing.T) (preparation.PublicationRequest, document.PublicationStartPlan, preparation.CompletionResult, contracts.Document) {
	w, p, chunks := fixture(t)
	a, _, err := preparation.Build(w, p, chunks)
	if err != nil {
		t.Fatal(err)
	}
	actor := contracts.Principal{SubjectID: "publisher", SubjectType: "user", Issuer: "identity"}
	w.Plan.ProfileDigest = p.Digest
	w.Plan.OriginalPrincipal = actor
	w.Plan.Result.Operation = contracts.PublicationOperation{ID: w.Job.OperationID, DocumentID: w.Revision.DocumentID, RevisionID: w.Revision.ID, ApprovalID: "approval", ActorID: actor.SubjectID}
	w.Plan.Result.Job = w.Job
	g := a.GenerationSet.Channels[contracts.KeywordChannel].ID
	job := w.Job
	job.State = "completed"
	job.GenerationID = &g
	r := preparation.PublicationRequest{OperationID: job.OperationID, ArtifactDigest: a.Digest, Command: contracts.CommandContext{Principal: actor, IdempotencyKey: "finalize", Request: contracts.RequestContext{RequestID: "trace", ProfileDigest: p.Digest}, Scope: contracts.AccessScope{SpaceID: w.Plan.SpaceID, KnowledgeBaseIDs: []contracts.ID{w.Plan.KnowledgeBaseID}, SubjectID: actor.SubjectID, Action: "publication.publish"}}}
	return r, w.Plan, preparation.CompletionResult{Job: job, Artifact: a, CompletedAt: time.Now().UTC()}, contracts.Document{ID: w.Revision.DocumentID, SpaceID: w.Plan.SpaceID, KnowledgeBaseID: w.Plan.KnowledgeBaseID, RowVersion: 1}
}
func TestPublicationPreservesEpochHistoryAndPrincipalBindings(t *testing.T) {
	r, p, c, d := publicationFixture(t)
	_, digest, err := preparation.PublicationDigests(r)
	if err != nil {
		t.Fatal(err)
	}
	v, err := preparation.PublicationFacts(r, p, c, d, "history", digest, time.Now().UTC())
	if err != nil || v.Operation.State != "committed" || v.History.PublicationEpoch != 1 || v.DocumentRowVersion != 2 || v.History.PreviousRevisionID != nil {
		t.Fatal("first publication", err)
	}
	oldRev, oldSet := contracts.ID("previous-revision"), contracts.ID("previous-set")
	d.ActiveRevisionID = &oldRev
	d.ActiveGenerationSetID = &oldSet
	d.PublicationEpoch = 1
	r.ExpectedEpoch = 1
	p.Result.Operation.ExpectedEpoch = 1
	v, err = preparation.PublicationFacts(r, p, c, d, "history-replacement", digest, time.Now().UTC())
	if err != nil || v.History.PreviousGenerationSetID == nil || *v.History.PreviousGenerationSetID != oldSet || v.History.PublicationEpoch != 2 {
		t.Fatal("replacement lost old source", err)
	}
	*v.History.PreviousGenerationSetID = "caller-change"
	if oldSet != "previous-set" {
		t.Fatal("publication aliased old source")
	}
	r, p, c, d = publicationFixture(t)
	r.Command.Principal.Issuer = "different-identity-source"
	if _, err := preparation.PublicationFacts(r, p, c, d, "history", digest, time.Now().UTC()); err == nil {
		t.Fatal("different original actor accepted")
	}
	r, p, c, d = publicationFixture(t)
	c.Artifact.Digest = contracts.Digest(strings.Repeat("b", 64))
	r.ArtifactDigest = c.Artifact.Digest
	if _, err := preparation.PublicationFacts(r, p, c, d, "history", digest, time.Now().UTC()); err == nil {
		t.Fatal("forged completion commitment accepted")
	}
}
func TestPublicationDigestIgnoresTraceButBindsSemanticInputs(t *testing.T) {
	r, _, _, _ := publicationFixture(t)
	actor, digest, err := preparation.PublicationDigests(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Command.Request.RequestID = "retry-trace"
	r.Command.Scope.ScopeID = "fresh-scope"
	r.Command.IdempotencyKey = "another-key"
	a, b, err := preparation.PublicationDigests(r)
	if err != nil || actor != a || digest != b {
		t.Fatal("transient values changed semantics", err)
	}
	r.ExpectedEpoch++
	_, changed, _ := preparation.PublicationDigests(r)
	if changed == digest {
		t.Fatal("epoch not committed")
	}
}

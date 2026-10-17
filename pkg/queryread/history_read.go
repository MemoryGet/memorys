package queryread

import (
	"context"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// ValidateHistoricalScopes does not authenticate scopes. It makes the distinct
// actions and resource/subject/policy intersection explicit without admitting a
// history action to the current-content read unit or any search executor.
func ValidateHistoricalScopes(ctx context.Context, q contracts.HistoricalContentReadRequest) error {
	r, h := q.Base, q.HistoryScope
	if ctx == nil || ctx.Err() != nil || q.Principal.SubjectID != r.Scope.SubjectID || q.Principal.Issuer == "" || q.Principal.ClientID == "" || (q.Principal.SubjectType != "user" && q.Principal.SubjectType != "service") || r.DocumentID == "" || r.RevisionID == "" || r.Request.RequestID == "" || r.Request.Deadline.IsZero() || !r.Request.Deadline.After(time.Now()) || r.Scope.Action != "knowledge.read" || h.Action != contracts.HistoricalReadAction || h.ScopeID == "" || r.Scope.ScopeID == "" || h.SubjectID == "" || h.SubjectID != r.Scope.SubjectID || h.SpaceID == "" || h.SpaceID != r.Scope.SpaceID || h.PolicyVersion == 0 || h.PolicyVersion != r.Scope.PolicyVersion || len(h.KnowledgeBaseIDs) != 1 || len(r.Scope.KnowledgeBaseIDs) != 1 || h.KnowledgeBaseIDs[0] == "" || h.KnowledgeBaseIDs[0] != r.Scope.KnowledgeBaseIDs[0] || !h.ExpiresAt.After(time.Now()) || !r.Scope.ExpiresAt.After(time.Now()) {
		return ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalid
	}
	return nil
}

// ValidateHistoricalContent checks the published-target review against trusted
// current review rules. It does not prove that a publication row exists.
func ValidateHistoricalContent(q contracts.HistoricalContentReadRequest, record contracts.HistoricalContentRecord, reviewPolicy uint64) error {
	r, a, v := record.Review.Revision, record.Review.Approval, record.Review.Validity
	if len(q.Base.Scope.KnowledgeBaseIDs) != 1 || record.PublicationID == "" || record.PublicationEpoch == 0 || record.PublicationEpoch >= record.Current.PublicationEpoch || record.StoreEpochID == "" || record.Current.DocumentID != q.Base.DocumentID || record.Current.SpaceID != q.Base.Scope.SpaceID || record.Current.KnowledgeBaseID != q.Base.Scope.KnowledgeBaseIDs[0] || record.Current.RevisionID == q.Base.RevisionID || record.Content.DocumentID != q.Base.DocumentID || record.Content.RevisionID != q.Base.RevisionID || r.DocumentID != q.Base.DocumentID || r.ID != q.Base.RevisionID || r.HashFormat != "akos.freeze/1" || !reviewDigest(r.ContentHash) || !reviewDigest(r.BlocksHash) || a.ID == "" || a.RevisionID != r.ID || a.ContentHash != r.ContentHash || a.PolicyVersion == 0 || a.RowVersion == 0 || a.ActorID == "" || a.DecidedAt.IsZero() || a.Decision != "approve" || reviewPolicy == 0 || v.ApprovalID != a.ID || v.ApprovalRowVersion != a.RowVersion || v.ReviewPolicyVersion != reviewPolicy || v.State != "effective" {
		return ErrInvalid
	}
	return nil
}

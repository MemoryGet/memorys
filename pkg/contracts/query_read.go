package contracts

import (
	"context"
	"time"
)

// ReadableDocumentLocator resolves ownership under current knowledge.read
// grants. Principal must be authenticated by the caller. It returns no content
// and must hide both nonexistent and unauthorized documents behind one error.
type ReadableDocumentLocator interface {
	LocateReadableDocument(context.Context, Principal, ID) (ReadableDocument, error)
}

type ReadableDocument struct {
	DocumentID, SpaceID, KnowledgeBaseID ID
	Scope                                AccessScope
}

// QueryReadIdentity is provider-neutral; it is not a vendor transaction ID or
// proof of consistency. Implementations must provide the promised observation.
// IsCurrentContentReadAction admits only current-content observation actions.
// It grants no authority and deliberately excludes history, writes and wildcards.
// Search executors must still require knowledge.search at their own boundary.
func IsCurrentContentReadAction(action Action) bool {
	return action == "knowledge.search" || action == "knowledge.read"
}

type QueryReadIdentity struct {
	StoreEpochID ID
	ReadVersion  string
	Watermark    string
}

type PublishedFact struct {
	DocumentID, SpaceID, KnowledgeBaseID, RevisionID ID
	RowVersion, PublicationEpoch                     uint64
	ApprovalID                                       ID
	ContentDigest                                    Digest
	GenerationSet                                    GenerationSet
}

type PublishedPageRequest struct {
	Identity         QueryReadIdentity
	KnowledgeBaseIDs []ID
	Cursor           string // Opaque, bound to identity/scope/order by the repository.
	Limit            int
}

type PublishedPage struct {
	Identity   QueryReadIdentity
	Facts      []PublishedFact
	NextCursor string
}

type CurrentDocumentState struct {
	Document             Document
	ActiveBackendEpochID ID
	GenerationSet        *GenerationSet
	PublicationReview    *CurrentPublicationReview
}

// CurrentPublicationReview is read in the same view as Document and its active
// generation set. It is a projection, not proof of byte integrity or authority.
// Missing review data must never be interpreted as a valid approval.
type CurrentPublicationReview struct {
	Revision FrozenRevisionFact
	Approval Approval
	Validity ApprovalValidityFact
}

type FrozenRevisionFact struct {
	ID, DocumentID          ID
	ContentHash, BlocksHash Digest
	HashFormat              string
}

// ReviewPolicyVersion is the rule version used to evaluate historical approval
// validity, distinct from both Approval.PolicyVersion and ACL PolicyVersion.
type ApprovalValidityFact struct {
	ApprovalID          ID
	ApprovalRowVersion  uint64
	ReviewPolicyVersion uint64
	State               string // effective, revoked, or stale; unknown fails closed.
}

// FrozenContentReader returns the standard akos.freeze/1 JSON payload for an
// immutable revision. It must enforce current scope and platform ID ownership.
// It is independent of parser/index provider types and vendor storage paths.
type FrozenContentReader interface {
	ReadFrozenContent(context.Context, FrozenContentReadRequest) (FrozenContentRecord, error)
}

type FrozenContentReadRequest struct {
	Request                RequestContext
	Scope                  AccessScope
	DocumentID, RevisionID ID
}

type FrozenContentRecord struct {
	DocumentID, RevisionID ID
	Payload                []byte // Standard JSON, not a vendor envelope; at most 4MiB.
}

// ReadContentVerifier checks every output block and its source against the
// approval-bound record. Unsupported source/content types must fail the whole
// read, never silently omit blocks. This port does not authorize a network send.
type ReadContentVerifier interface {
	VerifyReadContent(context.Context, FrozenContentReadRequest, FrozenRevisionFact, FrozenContentRecord) error
}

// ReadCursorState carries commitments only, never credentials or content.
// A valid signature is not authorization; the application must recompute the
// current identity/publication binding before continuing a page.
type ReadCursorState struct {
	Binding, Selection Digest
	Position, PageSize int
	ExpiresAt          time.Time
}

type ReadCursorCodec interface {
	SealReadCursor(context.Context, ReadCursorState) (string, error)
	OpenReadCursor(context.Context, string) (ReadCursorState, error)
}

// A repository is scoped to WithinRead's identity and authorized scope. It
// cannot write or escape the callback. Returned snapshots must be independent.
type QueryReadRepository interface {
	ReadPublishedPage(context.Context, PublishedPageRequest) (PublishedPage, error)
	LoadActiveEpoch(context.Context, ID) (BackendEpoch, error)
	LoadCurrentDocument(context.Context, ID) (CurrentDocumentState, error)
}

type QueryReadUnit interface {
	WithinRead(context.Context, AccessScope, func(context.Context, QueryReadIdentity, QueryReadRepository) error) error
}

// ValidatePublishedPage checks given relationships only. Scope provenance,
// current policy, pagination completeness/cursor binding, live readiness and
// transactional consistency require independent repository/application checks.
func ValidatePublishedPage(page PublishedPage, request PublishedPageRequest, scope AccessScope, afterDocumentID ID) error {
	invalid := func() error { return PublicError{Code: "validation_failed", SafeMessage: "Invalid published page"} }
	id := request.Identity
	if id.StoreEpochID == "" || id.ReadVersion == "" || id.Watermark == "" || page.Identity != id || request.Limit < 1 || request.Limit > 1000 || len(page.Facts) > request.Limit || len(request.KnowledgeBaseIDs) == 0 || len(request.KnowledgeBaseIDs) > 256 || scope.ScopeID == "" || scope.SubjectID == "" || scope.SpaceID == "" || !IsCurrentContentReadAction(scope.Action) || scope.PolicyVersion == 0 {
		return invalid()
	}
	allowed := map[ID]bool{}
	for _, kb := range scope.KnowledgeBaseIDs {
		allowed[kb] = true
	}
	selected := map[ID]bool{}
	for _, kb := range request.KnowledgeBaseIDs {
		if kb == "" || selected[kb] || !allowed[kb] {
			return invalid()
		}
		selected[kb] = true
	}
	if page.NextCursor != "" && (len(page.Facts) == 0 || page.NextCursor == request.Cursor) {
		return invalid()
	}
	previous := afterDocumentID
	seenRevisions := map[ID]bool{}
	seenSets := map[ID]bool{}
	seenGenerations := map[ID]bool{}
	for _, fact := range page.Facts {
		if fact.DocumentID == "" || fact.DocumentID <= previous || fact.SpaceID != scope.SpaceID || !selected[fact.KnowledgeBaseID] || fact.RevisionID == "" || seenRevisions[fact.RevisionID] || fact.RowVersion == 0 || fact.PublicationEpoch == 0 || fact.ApprovalID == "" || !validDigest(fact.ContentDigest) {
			return invalid()
		}
		previous = fact.DocumentID
		seenRevisions[fact.RevisionID] = true
		set := fact.GenerationSet
		if set.ID == "" || seenSets[set.ID] || set.RevisionID != fact.RevisionID || set.BackendEpochID == "" || set.State != "ready" || len(set.Channels) < 1 || len(set.Channels) > 3 {
			return invalid()
		}
		seenSets[set.ID] = true
		for channel, g := range set.Channels {
			if !knownChannel(channel) || g.Channel != channel || g.ID == "" || seenGenerations[g.ID] || g.RevisionID != fact.RevisionID || g.KnowledgeBaseID != fact.KnowledgeBaseID || g.BackendEpochID != set.BackendEpochID || g.State != "ready" || !validDigest(g.ProfileDigest) {
				return invalid()
			}
			seenGenerations[g.ID] = true
		}
	}
	return nil
}

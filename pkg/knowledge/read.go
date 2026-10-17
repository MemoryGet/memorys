package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var ErrRead = errors.New("knowledge read unavailable")

type CurrentReadDependencies struct {
	Auth       contracts.Authenticator
	Locator    contracts.ReadableDocumentLocator
	Reads      contracts.QueryReadUnit
	Content    contracts.FrozenContentReader
	Facts      queryread.CurrentFactsSource
	Verifier   contracts.ReadContentVerifier
	Pagination *ReadPagination
	History    *HistoryReadDependencies
}

// CurrentReadService returns an owned standard frozen payload to platform code.
// All output sources are verified before the final identity/publication check.
// Historical targets additionally require an explicitly configured history
// reader/verifier and an independent current resource grant.
type CurrentReadService struct{ deps CurrentReadDependencies }

func NewCurrentRead(d CurrentReadDependencies) (*CurrentReadService, error) {
	if missing(d.Auth) || missing(d.Locator) || missing(d.Reads) || missing(d.Content) || missing(d.Facts) || missing(d.Verifier) {
		return nil, ErrRead
	}
	if d.Pagination != nil {
		p := *d.Pagination
		if missing(p.Codec) || p.PageSize < 1 || p.PageSize > 1000 || p.TTL < time.Second || p.TTL > 5*time.Minute {
			return nil, ErrRead
		}
		d.Pagination = &p
	}
	if d.History != nil {
		h := *d.History
		if missing(h.Authorizer) || missing(h.Content) || missing(h.Verifier) {
			return nil, ErrRead
		}
		d.History = &h
	}
	return &CurrentReadService{deps: d}, nil
}

type CurrentReadInput struct {
	Credential contracts.Credential
	Request    contracts.RequestContext
	DocumentID contracts.ID
	RevisionID contracts.ID // Optional; older published targets require History dependencies and an additional grant.
}

func (s *CurrentReadService) ReadCurrent(ctx context.Context, in CurrentReadInput) (contracts.FrozenContentRecord, error) {
	result, err := s.readSnapshot(ctx, in, nil)
	return result.Record, err
}

type currentReadSnapshot struct {
	Record    contracts.FrozenContentRecord
	Binding   contracts.Digest
	ExpiresAt time.Time
}

func (s *CurrentReadService) readSnapshot(ctx context.Context, in CurrentReadInput, beforeFinal func(currentReadSnapshot) error) (currentReadSnapshot, error) {
	var empty currentReadSnapshot
	if s == nil || ctx == nil || ctx.Err() != nil || !id(in.DocumentID) || in.RevisionID != "" && !id(in.RevisionID) || !id(in.Request.RequestID) || in.Request.Deadline.IsZero() || !in.Request.Deadline.After(time.Now()) || in.Credential.Kind == "" || len(in.Credential.Secret) == 0 || len(in.Credential.Secret) > 16384 {
		return empty, ErrRead
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	deadline, _ := ctx.Deadline()
	in.Request.Deadline = deadline
	secret := append([]byte(nil), in.Credential.Secret...)
	defer clear(secret)
	authenticate := func() (contracts.Principal, error) {
		credential := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), secret...)}
		defer clear(credential.Secret)
		p, e := s.deps.Auth.Authenticate(ctx, credential)
		if e != nil || ctx.Err() != nil {
			return contracts.Principal{}, ErrRead
		}
		return copyValue(p)
	}
	who, err := authenticate()
	if err != nil {
		return empty, ErrRead
	}
	locate := func(p contracts.Principal) (contracts.ReadableDocument, error) {
		actor, e := copyValue(p)
		if e != nil {
			return contracts.ReadableDocument{}, e
		}
		l, e := s.deps.Locator.LocateReadableDocument(ctx, actor, in.DocumentID)
		if e != nil || ctx.Err() != nil {
			return contracts.ReadableDocument{}, ErrRead
		}
		l, e = copyValue(l)
		if e != nil || l.DocumentID != in.DocumentID || !id(l.SpaceID) || !id(l.KnowledgeBaseID) || l.Scope.SpaceID != l.SpaceID || l.Scope.Action != "knowledge.read" || len(l.Scope.KnowledgeBaseIDs) != 1 || l.Scope.KnowledgeBaseIDs[0] != l.KnowledgeBaseID {
			return contracts.ReadableDocument{}, ErrRead
		}
		if queryread.MatchRefreshedScope(ctx, p, l.Scope, p, l.Scope, []contracts.ID{l.KnowledgeBaseID}) != nil {
			return contracts.ReadableDocument{}, ErrRead
		}
		return l, nil
	}
	location, err := locate(who)
	if err != nil {
		return empty, ErrRead
	}
	var fact contracts.PublishedFact
	var storeEpoch contracts.ID
	var blocksHash contracts.Digest
	var reviewPolicies map[contracts.ID]uint64
	unit, err := queryread.ProtectUnit(s.deps.Reads)
	if err != nil {
		return empty, ErrRead
	}
	initial, err := queryread.ObserveCurrent(ctx, unit, location.Scope, []contracts.ID{in.DocumentID}, queryread.CurrentLimits{MaxDocuments: 1, MaxBytes: 4 << 20})
	if err != nil || len(initial.Documents) != 1 {
		return empty, ErrRead
	}
	storeEpoch = initial.Identity.StoreEpochID
	fact, err = queryread.PublishedCurrentFact(location.Scope, initial.Identity, initial.Documents[0])
	if err != nil || fact.DocumentID != in.DocumentID {
		return empty, ErrRead
	}
	historical := in.RevisionID != "" && in.RevisionID != fact.RevisionID
	if historical && s.deps.History == nil {
		return empty, ErrRead
	}
	check := func(scope contracts.AccessScope) error {
		owned, e := copyValue(scope)
		if e != nil {
			return e
		}
		facts, e := s.deps.Facts.LoadCurrentFacts(ctx, in.Request, owned)
		if e != nil || facts.ProfileDigest != in.Request.ProfileDigest {
			return ErrRead
		}
		facts, e = copyValue(facts)
		if e != nil {
			return e
		}
		observation, e := queryread.ObserveCurrent(ctx, unit, scope, []contracts.ID{in.DocumentID}, queryread.CurrentLimits{MaxDocuments: 1, MaxBytes: 4 << 20})
		if e != nil {
			return e
		}
		expect := queryread.CurrentExpectation{StoreEpochID: storeEpoch, ScopeVersion: scope.PolicyVersion, Published: []contracts.PublishedFact{fact}}
		if queryread.MatchCurrentReview(scope, expect, observation, facts.ReviewPolicies) != nil {
			return ErrRead
		}
		if len(observation.Documents) != 1 || observation.Documents[0].PublicationReview == nil {
			return ErrRead
		}
		actual := observation.Documents[0].PublicationReview.Revision.BlocksHash
		if blocksHash != "" && blocksHash != actual {
			return ErrRead
		}
		blocksHash = actual
		reviewPolicies = facts.ReviewPolicies
		return nil
	}
	if check(location.Scope) != nil {
		return empty, ErrRead
	}
	contentRequest, err := copyValue(contracts.FrozenContentReadRequest{Request: in.Request, Scope: location.Scope, DocumentID: in.DocumentID, RevisionID: fact.RevisionID})
	if err != nil {
		return empty, ErrRead
	}
	var record contracts.FrozenContentRecord
	var historicalRequest contracts.HistoricalContentReadRequest
	var historicalRecord *contracts.HistoricalContentRecord
	expected := contracts.FrozenRevisionFact{ID: fact.RevisionID, DocumentID: in.DocumentID, ContentHash: fact.ContentDigest, BlocksHash: blocksHash, HashFormat: canonical.FrozenFormat}
	if historical {
		historicalRequest, err = s.historyRequest(ctx, in, who, location)
		if err != nil {
			return empty, ErrRead
		}
		sent, e := copyValue(historicalRequest)
		if e != nil {
			return empty, ErrRead
		}
		actual, e := s.deps.History.Content.ReadHistoricalContent(ctx, sent)
		if e != nil || len(actual.Content.Payload) > 4<<20 {
			return empty, ErrRead
		}
		actual, e = copyValue(actual)
		if e != nil || validateHistoryResult(historicalRequest, actual, fact, storeEpoch, reviewPolicies[location.KnowledgeBaseID]) != nil {
			return empty, ErrRead
		}
		historicalRecord = &actual
		record, expected = actual.Content, actual.Review.Revision
		contentRequest = historicalRequest.Base
	} else {
		readerRequest, e := copyValue(contentRequest)
		if e != nil {
			return empty, ErrRead
		}
		record, err = s.deps.Content.ReadFrozenContent(ctx, readerRequest)
	}
	if err != nil || record.DocumentID != in.DocumentID || record.RevisionID != expected.ID || len(record.Payload) > 4<<20 {
		return empty, ErrRead
	}
	record.Payload = append([]byte(nil), record.Payload...)
	hashes, err := canonical.Frozen(record.Payload)
	if err != nil || contracts.Digest(hashes.ContentHash) != expected.ContentHash || contracts.Digest(hashes.BlocksHash) != expected.BlocksHash {
		return empty, ErrRead
	}
	verifyRequest, err := copyValue(contentRequest)
	if err != nil {
		return empty, ErrRead
	}
	verifyRecord := record
	verifyRecord.Payload = append([]byte(nil), record.Payload...)
	if historical {
		ownedRequest, e := copyValue(historicalRequest)
		if e != nil {
			return empty, ErrRead
		}
		ownedRecord, e := copyValue(*historicalRecord)
		if e != nil {
			return empty, ErrRead
		}
		err = s.deps.History.Verifier.VerifyHistoricalContent(ctx, ownedRequest, ownedRecord)
	} else {
		err = s.deps.Verifier.VerifyReadContent(ctx, verifyRequest, expected, verifyRecord)
	}
	if err != nil || ctx.Err() != nil {
		return empty, ErrRead
	}
	var boundHistory *contracts.HistoricalContentRecord
	if historical {
		meta := historyMeta(*historicalRecord)
		boundHistory = &meta
	}
	// Stable cross-request binding excludes ephemeral scope IDs/expirations and
	// observation watermarks, while retaining identity, ACL and publication facts.
	raw, err := json.Marshal(struct {
		Principal                   contracts.Principal
		Space, KnowledgeBase, Store contracts.ID
		Policy                      uint64
		Profile                     contracts.Digest
		Published                   contracts.PublishedFact
		Revision                    contracts.FrozenRevisionFact
		Historical                  *contracts.HistoricalContentRecord `json:"Historical,omitempty"`
	}{who, location.SpaceID, location.KnowledgeBaseID, storeEpoch, location.Scope.PolicyVersion, in.Request.ProfileDigest, fact, expected, boundHistory})
	if err != nil {
		return empty, ErrRead
	}
	raw, err = canonical.JSON(raw)
	if err != nil {
		return empty, ErrRead
	}
	digest := sha256.Sum256(append([]byte("akos.read-snapshot/1\n"), raw...))
	snapshot := currentReadSnapshot{Record: record, Binding: contracts.Digest(hex.EncodeToString(digest[:])), ExpiresAt: location.Scope.ExpiresAt}
	if historical && historicalRequest.HistoryScope.ExpiresAt.Before(snapshot.ExpiresAt) {
		snapshot.ExpiresAt = historicalRequest.HistoryScope.ExpiresAt
	}
	if beforeFinal != nil {
		owned := snapshot
		owned.Record.Payload = append([]byte(nil), record.Payload...)
		if err := beforeFinal(owned); err != nil {
			return empty, err
		}
	}
	current, err := authenticate()
	if err != nil || !reflect.DeepEqual(current, who) {
		return empty, ErrRead
	}
	fresh, err := locate(current)
	if err != nil || fresh.SpaceID != location.SpaceID || fresh.KnowledgeBaseID != location.KnowledgeBaseID || queryread.MatchRefreshedScope(ctx, who, location.Scope, current, fresh.Scope, []contracts.ID{location.KnowledgeBaseID}) != nil || check(fresh.Scope) != nil || ctx.Err() != nil {
		return empty, ErrRead
	}
	snapshot.ExpiresAt = fresh.Scope.ExpiresAt
	if historical {
		freshRequest, e := s.historyRequest(ctx, in, current, fresh)
		if e != nil {
			return empty, ErrRead
		}
		sent, e := copyValue(freshRequest)
		if e != nil {
			return empty, ErrRead
		}
		actual, e := s.deps.History.Content.ReadHistoricalContent(ctx, sent)
		if e != nil || len(actual.Content.Payload) > 4<<20 {
			return empty, ErrRead
		}
		actual, e = copyValue(actual)
		if e != nil || validateHistoryResult(freshRequest, actual, fact, storeEpoch, reviewPolicies[fresh.KnowledgeBaseID]) != nil || !reflect.DeepEqual(historyMeta(actual), *boundHistory) || ctx.Err() != nil {
			return empty, ErrRead
		}
		if freshRequest.HistoryScope.ExpiresAt.Before(snapshot.ExpiresAt) {
			snapshot.ExpiresAt = freshRequest.HistoryScope.ExpiresAt
		}
	}
	return snapshot, nil
}

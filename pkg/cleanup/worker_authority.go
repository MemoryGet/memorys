package cleanup

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// WorkerAuthority is local to one sequential consumer. It owns the credential,
// one current cycle scope and one current index request; it is not a Grant token.
type WorkerAuthority struct {
	mu                sync.Mutex
	closed            bool
	credential        contracts.Credential
	auth              contracts.Authenticator
	authorizer        contracts.Authorizer
	principal         contracts.Principal
	worker, space, kb contracts.ID
	execution, index  contracts.AccessScope
	indexDocument     contracts.ID
	indexOperation    contracts.CleanupOperation
	admission         func(context.Context, contracts.AccessScope, contracts.CleanupOperation) error
}

type WorkerAuthorityConfig struct {
	Credential                         contracts.Credential
	Auth                               contracts.Authenticator
	Authorizer                         contracts.Authorizer
	WorkerID, SpaceID, KnowledgeBaseID contracts.ID
	// Required independent current lease, source and module/target binding check.
	VerifyOperation func(context.Context, contracts.AccessScope, contracts.CleanupOperation) error
}

func missingWorkerProvider(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}

func NewWorkerAuthority(ctx context.Context, c WorkerAuthorityConfig) (*WorkerAuthority, error) {
	if ctx == nil || missingWorkerProvider(c.Auth) || missingWorkerProvider(c.Authorizer) || c.VerifyOperation == nil || !validID(c.WorkerID) || !validID(c.SpaceID) || !validID(c.KnowledgeBaseID) || c.Credential.Kind != "access_token" || len(c.Credential.Secret) == 0 || len(c.Credential.Secret) > 16<<10 {
		return nil, ErrRejected
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrRejected
	}
	p := &WorkerAuthority{auth: c.Auth, authorizer: c.Authorizer, worker: c.WorkerID, space: c.SpaceID, kb: c.KnowledgeBaseID, admission: c.VerifyOperation, credential: contracts.Credential{Kind: c.Credential.Kind, Secret: append([]byte(nil), c.Credential.Secret...)}}
	who, e := p.authenticate(ctx)
	if e != nil || who.SubjectType != "service" || who.SubjectID != c.WorkerID || !validID(who.ClientID) || who.Issuer == "" || who.Delegation != nil {
		p.Close()
		return nil, ErrRejected
	}
	p.principal = who
	return p, nil
}
func (p *WorkerAuthority) authenticate(ctx context.Context) (contracts.Principal, error) {
	if ctx == nil || p.closed || missingWorkerProvider(p.auth) || missingWorkerProvider(p.authorizer) || ctx.Err() != nil {
		return contracts.Principal{}, ErrRejected
	}
	if _, ok := ctx.Deadline(); !ok {
		return contracts.Principal{}, ErrRejected
	}
	c := contracts.Credential{Kind: p.credential.Kind, Secret: append([]byte(nil), p.credential.Secret...)}
	defer clear(c.Secret)
	who, e := p.auth.Authenticate(ctx, c)
	if e != nil || ctx.Err() != nil {
		return contracts.Principal{}, ErrRejected
	}
	return ownPollValue(who)
}
func (p *WorkerAuthority) grant(ctx context.Context, action contracts.Action, doc contracts.ID) (contracts.AccessScope, error) {
	who, e := p.authenticate(ctx)
	if e != nil || !reflect.DeepEqual(who, p.principal) {
		return contracts.AccessScope{}, ErrRejected
	}
	q := contracts.AuthorizeRequest{Principal: who, Action: action, SpaceID: p.space, KnowledgeBaseIDs: []contracts.ID{p.kb}}
	if doc != "" {
		q.ResourceIDs = []contracts.ID{doc}
	}
	s, e := p.authorizer.Authorize(ctx, q)
	s, copyErr := ownPollValue(s)
	if e != nil || copyErr != nil || ctx.Err() != nil || !validID(s.ScopeID) || s.SubjectID != p.worker || s.Action != action || s.SpaceID != p.space || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != p.kb || s.PolicyVersion == 0 || !s.ExpiresAt.After(time.Now()) {
		return contracts.AccessScope{}, ErrRejected
	}
	d, _ := ctx.Deadline()
	if s.ExpiresAt.After(d) {
		return contracts.AccessScope{}, ErrRejected
	}
	return s, nil
}
func (p *WorkerAuthority) Scope(ctx context.Context) (contracts.AccessScope, error) {
	if p == nil {
		return contracts.AccessScope{}, ErrRejected
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s, e := p.grant(ctx, "publication.cleanup.execute", "")
	if e != nil {
		return contracts.AccessScope{}, e
	}
	p.execution = s
	p.index = contracts.AccessScope{}
	p.indexDocument = ""
	p.indexOperation = contracts.CleanupOperation{}
	return ownPollValue(s)
}

// CurrentScope rechecks the current cycle without replacing it or invalidating
// an index request. Module resolution must not start another polling cycle.
func (p *WorkerAuthority) CurrentScope(ctx context.Context) (contracts.AccessScope, error) {
	if p == nil {
		return contracts.AccessScope{}, ErrRejected
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.execution.Action != "publication.cleanup.execute" || p.verify(ctx, p.execution, "") != nil {
		return contracts.AccessScope{}, ErrRejected
	}
	return ownPollValue(p.execution)
}
func (p *WorkerAuthority) verify(ctx context.Context, s contracts.AccessScope, doc contracts.ID) error {
	if !s.ExpiresAt.After(time.Now()) {
		return ErrRejected
	}
	current, e := p.grant(ctx, s.Action, doc)
	if e != nil || current.PolicyVersion != s.PolicyVersion || !s.ExpiresAt.After(time.Now()) {
		return ErrRejected
	}
	return nil
}
func (p *WorkerAuthority) VerifyScope(ctx context.Context, s contracts.AccessScope) error {
	if p == nil {
		return ErrRejected
	}
	p.mu.Lock()
	if reflect.DeepEqual(s, p.execution) && s.Action == "publication.cleanup.execute" {
		defer p.mu.Unlock()
		return p.verify(ctx, s, "")
	}
	if !reflect.DeepEqual(s, p.index) || s.Action != "index.delete" {
		p.mu.Unlock()
		return ErrRejected
	}
	execution, _ := ownPollValue(p.execution)
	op := copyOperation(p.indexOperation)
	p.mu.Unlock()
	if p.admission(ctx, execution, op) != nil {
		return ErrRejected
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !reflect.DeepEqual(execution, p.execution) || !reflect.DeepEqual(s, p.index) {
		return ErrRejected
	}
	return p.verify(ctx, s, p.indexDocument)
}
func (p *WorkerAuthority) AuthorizeJob(ctx context.Context, s contracts.AccessScope, r contracts.ResourceJob) error {
	if p == nil {
		return ErrRejected
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !reflect.DeepEqual(s, p.execution) || s.Action != "publication.cleanup.execute" || r.Job.SpaceID != p.space || !validID(r.Job.ID) || !validID(r.ResourceID) || !validDigest(r.Job.ProfileDigest) {
		return ErrRejected
	}
	return p.verify(ctx, s, r.ResourceID)
}

// IndexRequest obtains a separate actual index.delete decision after independent
// operation admission. It never changes the action on an execution scope.
func (p *WorkerAuthority) IndexRequest(ctx context.Context, op contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
	var empty contracts.IndexDeleteRequest
	if p == nil || ctx == nil {
		return empty, ErrRejected
	}
	op, e := ownPollValue(op)
	if e != nil {
		return empty, e
	}
	expected, e := Operation(contracts.CleanupLease{SpaceID: op.SpaceID, KnowledgeBaseID: op.KnowledgeBaseID, DocumentID: op.DocumentID, PlanID: op.PlanID, TargetID: op.TargetID, RevisionID: op.RevisionID, GenerationSetID: op.GenerationSetID, PublicationEpoch: op.PublicationEpoch, Target: op.Target})
	if e != nil || !reflect.DeepEqual(expected, op) || op.SpaceID != p.space || op.KnowledgeBaseID != p.kb || op.Target.Kind != "index" {
		return empty, ErrRejected
	}
	p.mu.Lock()
	scope, _ := ownPollValue(p.execution)
	closed := p.closed
	p.mu.Unlock()
	if closed || scope.Action != "publication.cleanup.execute" || p.admission(ctx, scope, copyOperation(op)) != nil {
		return empty, ErrRejected
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !reflect.DeepEqual(scope, p.execution) || p.verify(ctx, scope, op.DocumentID) != nil {
		return empty, ErrRejected
	}
	s, e := p.grant(ctx, "index.delete", op.DocumentID)
	if e != nil || s.PolicyVersion != scope.PolicyVersion {
		return empty, ErrRejected
	}
	p.index = s
	p.indexDocument = op.DocumentID
	p.indexOperation = op
	d, _ := ctx.Deadline()
	return contracts.IndexDeleteRequest{Request: contracts.RequestContext{RequestID: op.ID, Deadline: d, ProfileDigest: op.Target.ProfileDigest}, Scope: s, GenerationID: *op.Target.GenerationID}, nil
}
func (p *WorkerAuthority) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	clear(p.credential.Secret)
	p.execution = contracts.AccessScope{}
	p.index = contracts.AccessScope{}
	p.indexDocument = ""
	p.indexOperation = contracts.CleanupOperation{}
}
func (p *WorkerAuthority) String() string   { return "WorkerAuthority([redacted])" }
func (p *WorkerAuthority) GoString() string { return p.String() }

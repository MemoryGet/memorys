package withdrawal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type fakeUnit struct {
	ports contracts.WithdrawalPorts
	after error
	calls int
}

func (f *fakeUnit) WithinWithdrawal(c context.Context, s contracts.AccessScope, fn func(context.Context, contracts.WithdrawalPorts) error) error {
	f.calls++
	if err := fn(c, f.ports); err != nil {
		return err
	}
	return f.after
}

type fakeHistory struct {
	writes    int
	principal contracts.Principal
}

func (*fakeHistory) Load(context.Context, contracts.AccessScope, contracts.ID) (contracts.PublicationHistory, error) {
	return contracts.PublicationHistory{}, errors.New("not expected")
}
func (h *fakeHistory) Append(_ context.Context, _ contracts.AccessScope, v contracts.PublicationHistory) error {
	h.writes++
	h.principal = v.Actor
	return nil
}

type fakePlans struct{ writes int }

func (*fakePlans) Load(context.Context, contracts.AccessScope, contracts.ID) (contracts.CleanupPlan, error) {
	return contracts.CleanupPlan{}, errors.New("not expected")
}
func (p *fakePlans) Append(context.Context, contracts.AccessScope, contracts.CleanupPlan) error {
	p.writes++
	return nil
}

func fixture() (contracts.WithdrawRequest, Preparation, contracts.WithdrawalSnapshot) {
	now := time.Now().UTC()
	digest := contracts.Digest(strings.Repeat("a", 64))
	request := contracts.WithdrawRequest{DocumentID: "doc", ExpectedEpoch: 1, Reason: "withdraw", Command: contracts.CommandContext{IdempotencyKey: "key", Principal: contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "test", Delegation: &contracts.Delegation{UserID: "verified-user", ClientID: "client", ProofReference: "proof"}}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, Action: "publication.withdraw", ExpiresAt: now.Add(time.Minute)}, Request: contracts.RequestContext{RequestID: "request", ProfileDigest: digest, Deadline: now.Add(time.Minute)}}}
	prepared := Preparation{OperationID: "operation", HistoryID: "history", PlanID: "plan", EventID: "event", AuditID: "audit", ActorSemanticDigest: digest, CommandDigest: digest, OccurredAt: now}
	g, e, ch := contracts.ID("generation"), contracts.ID("backend"), contracts.KeywordChannel
	snapshot := contracts.WithdrawalSnapshot{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", RevisionID: "revision", GenerationSetID: "set", PublicationEpoch: 1, RowVersion: 1, Targets: []contracts.CleanupTarget{{ID: "target", Kind: "index", ModuleInstanceID: "engine", BindingReference: "binding", ProfileDigest: digest, GenerationID: &g, BackendEpochID: &e, Channel: &ch}}}
	return request, prepared, snapshot
}
func TestCommitFailureReturnsNoResultAndPreparationCannotMutateRequest(t *testing.T) {
	r, p, current := fixture()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h, plans := &fakeHistory{}, &fakePlans{}
	commitFailure := errors.New("commit outcome unknown")
	u := &fakeUnit{after: commitFailure, ports: contracts.WithdrawalPorts{History: h, Plans: plans, Reserve: func(context.Context, contracts.AccessScope, contracts.WithdrawalCommand) (contracts.CommandReservation, error) {
		return contracts.CommandReservation{Created: true, OperationID: "operation"}, nil
	}, LoadCurrent: func(context.Context, contracts.AccessScope, contracts.ID) (contracts.WithdrawalSnapshot, error) {
		return current, nil
	}, Complete: func(context.Context, contracts.AccessScope, contracts.WithdrawalCompletion) error { return nil }}}
	s := Service{Transactions: u, Prepare: func(_ context.Context, input contracts.WithdrawRequest) (Preparation, error) {
		input.Command.Scope.KnowledgeBaseIDs[0] = "other"
		input.Command.Principal.Delegation.UserID = "other"
		return p, nil
	}}
	result, err := s.Withdraw(ctx, r)
	if !errors.Is(err, commitFailure) || result != (contracts.WithdrawResult{}) || h.writes != 1 || plans.writes != 1 {
		t.Fatal("commit failure reported as success", result, err)
	}
	if h.principal.Delegation.UserID != "verified-user" || r.Command.Scope.KnowledgeBaseIDs[0] != "kb" {
		t.Fatal("preparation mutated frozen identity/scope")
	}
}
func TestSnapshotMismatchStopsWrites(t *testing.T) {
	r, p, current := fixture()
	current.PublicationEpoch = 2
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h, plans := &fakeHistory{}, &fakePlans{}
	u := &fakeUnit{ports: contracts.WithdrawalPorts{History: h, Plans: plans, Reserve: func(context.Context, contracts.AccessScope, contracts.WithdrawalCommand) (contracts.CommandReservation, error) {
		return contracts.CommandReservation{Created: true, OperationID: "operation"}, nil
	}, LoadCurrent: func(context.Context, contracts.AccessScope, contracts.ID) (contracts.WithdrawalSnapshot, error) {
		return current, nil
	}, Complete: func(context.Context, contracts.AccessScope, contracts.WithdrawalCompletion) error {
		t.Fatal("CAS called on stale head")
		return nil
	}}}
	_, err := (Service{Transactions: u, Prepare: func(context.Context, contracts.WithdrawRequest) (Preparation, error) { return p, nil }}).Withdraw(ctx, r)
	if !errors.Is(err, ErrRejected) || h.writes != 0 || plans.writes != 0 {
		t.Fatal("stale head progressed", err)
	}
}
func TestPreparationDenialDoesNotOpenTransaction(t *testing.T) {
	r, _, _ := fixture()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	u := &fakeUnit{}
	denied := errors.New("authority denied")
	_, err := (Service{Transactions: u, Prepare: func(context.Context, contracts.WithdrawRequest) (Preparation, error) { return Preparation{}, denied }}).Withdraw(ctx, r)
	if !errors.Is(err, denied) || u.calls != 0 {
		t.Fatal("unverified command entered transaction")
	}
}

package queryread_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type checkControl struct {
	log    *[]string
	states []queryread.CurrentControlState
	calls  int
	fail   bool
}

func (c *checkControl) Refresh(_ context.Context, p contracts.Principal, _ contracts.RequestContext) (queryread.CurrentControlState, error) {
	*c.log = append(*c.log, "control")
	if c.fail {
		return queryread.CurrentControlState{}, errors.New("secret token detail")
	}
	s := c.states[c.calls]
	c.calls++
	if p.Delegation != nil {
		p.Delegation.UserID = "mutated argument"
	}
	return s, nil
}

type checkBackend struct {
	log     *[]string
	fail    bool
	change  func()
	inspect func([]contracts.Evidence)
}

func (b checkBackend) Verify(_ context.Context, _ contracts.RequestContext, s contracts.AccessScope, _ queryread.CurrentObservation, evidence []contracts.Evidence) error {
	*b.log = append(*b.log, "backend")
	if b.inspect != nil {
		b.inspect(evidence)
	}
	s.KnowledgeBaseIDs[0] = "mutated argument"
	if len(evidence) > 0 {
		evidence[0].SourceExcerpt = "mutated backend excerpt"
		if evidence[0].Citation.Source.Locator.Characters != nil {
			evidence[0].Citation.Source.Locator.Characters.End++
		}
	}
	if b.change != nil {
		b.change()
	}
	if b.fail {
		return errors.New("secret backend detail")
	}
	return nil
}

type checkRead struct {
	currentRepository
	state contracts.CurrentDocumentState
	log   *[]string
	fail  bool
}

func (r *checkRead) LoadCurrentDocument(context.Context, contracts.ID) (contracts.CurrentDocumentState, error) {
	return r.state, nil
}
func (r *checkRead) WithinRead(ctx context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	*r.log = append(*r.log, "read")
	if r.fail {
		return errors.New("secret database detail")
	}
	return cb(ctx, contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "fresh", Watermark: "current"}, r)
}
func checkFixture(log *[]string) (queryread.CheckOperation, *checkControl, *checkRead, checkBackend) {
	e, o := currentReviewFixture()
	d := contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	op := queryread.CheckOperation{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: d, Deadline: time.Now().Add(time.Minute)}, Principal: refreshPrincipal(), Scope: currentScope(), Expectation: e}
	c := &checkControl{log: log, states: []queryread.CurrentControlState{
		{Principal: refreshPrincipal(), Scope: currentScope(), ProfileDigest: d, ReviewPolicies: map[contracts.ID]uint64{"kb": 7}},
		{Principal: refreshPrincipal(), Scope: currentScope(), ProfileDigest: d, ReviewPolicies: map[contracts.ID]uint64{"kb": 7}},
	}}
	return op, c, &checkRead{state: o.Documents[0], log: log}, checkBackend{log: log}
}

var checkLimits = queryread.CurrentLimits{MaxDocuments: 10, MaxBytes: 1 << 20}

func TestCheckCurrentOrdersChecksAndIsolatesArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var log []string
	op, c, r, b := checkFixture(&log)
	if err := queryread.CheckCurrent(ctx, op, c, r, b, checkLimits); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log, []string{"control", "read", "backend", "control"}) {
		t.Fatal(log)
	}
	if op.Principal.Delegation.UserID != "user" || op.Scope.KnowledgeBaseIDs[0] != "kb" {
		t.Fatal("provider mutated original operation")
	}
	log = nil
	op, c, r, b = checkFixture(&log)
	op.Principal.Issuer = string([]byte{0xff})
	if queryread.CheckCurrent(ctx, op, c, r, b, checkLimits) != queryread.ErrInvalid || len(log) != 0 {
		t.Fatal("invalid identity encoding repaired into an operation")
	}
}
func TestCheckCurrentStopsAtEachRequiredFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, stage := range []string{"control", "read", "approval", "backend"} {
		t.Run(stage, func(t *testing.T) {
			var log []string
			op, c, r, b := checkFixture(&log)
			want := []string{"control"}
			expected := error(queryread.ErrCheck)
			switch stage {
			case "control":
				c.fail = true
			case "read":
				r.fail = true
				want = append(want, "read")
			case "approval":
				r.state.PublicationReview.Validity.State = "revoked"
				want = append(want, "read")
				expected = queryread.ErrStale
			case "backend":
				b.fail = true
				want = append(want, "read", "backend")
			}
			if err := queryread.CheckCurrent(ctx, op, c, r, b, checkLimits); err != expected {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(log, want) {
				t.Fatal(log)
			}
		})
	}
}
func TestCheckCurrentDetectsControlChangesDuringBackendVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"acl", "review", "config", "actor"} {
		t.Run(kind, func(t *testing.T) {
			var log []string
			op, c, r, b := checkFixture(&log)
			b.change = func() {
				s := &c.states[1]
				switch kind {
				case "acl":
					s.Scope.PolicyVersion++
				case "review":
					s.ReviewPolicies["kb"]++
				case "config":
					s.ProfileDigest = contracts.Digest("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
				case "actor":
					s.Principal.SubjectID = "other"
					s.Scope.SubjectID = "other"
				}
			}
			if queryread.CheckCurrent(ctx, op, c, r, b, checkLimits) != queryread.ErrStale {
				t.Fatal("changed control state accepted")
			}
		})
	}
}
func TestCheckCurrentEmptyEvidenceAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var log []string
	op, c, _, _ := checkFixture(&log)
	op.Expectation.Published = nil
	if err := queryread.CheckCurrent(ctx, op, c, nil, nil, checkLimits); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log, []string{"control", "control"}) {
		t.Fatal(log)
	}
	log = nil
	op, c, r, b := checkFixture(&log)
	b.change = cancel
	if queryread.CheckCurrent(ctx, op, c, r, b, checkLimits) != context.Canceled {
		t.Fatal("cancellation ignored")
	}
	if !reflect.DeepEqual(log, []string{"control", "read", "backend"}) {
		t.Fatal(log)
	}
	log = nil
	op, c, r, b = checkFixture(&log)
	op.Request.Deadline = time.Now().Add(-time.Second)
	bounded, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	if queryread.CheckCurrent(bounded, op, c, r, b, checkLimits) != context.DeadlineExceeded || len(log) != 0 {
		t.Fatal("expired parent started calls")
	}
}

package queryread

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrCheck = errors.New("current verification unavailable")

type CheckOperation struct {
	Request     contracts.RequestContext
	Principal   contracts.Principal
	Scope       contracts.AccessScope
	Expectation CurrentExpectation
	Evidence    []contracts.Evidence // Exact batch to be verified before downstream use.
}

type CurrentControlState struct {
	Principal      contracts.Principal
	Scope          contracts.AccessScope
	ProfileDigest  contracts.Digest
	ReviewPolicies map[contracts.ID]uint64
}

// CurrentControl must authenticate the original operation and fetch current
// authorization/configuration/review rules, never derive permission from args.
type CurrentControl interface {
	Refresh(context.Context, contracts.Principal, contracts.RequestContext) (CurrentControlState, error)
}

// CurrentBackend verifies actual readiness and frozen content/source integrity.
// It must not interpret metadata's ready string as proof of availability.
type CurrentBackend interface {
	Verify(context.Context, contracts.RequestContext, contracts.AccessScope, CurrentObservation, []contracts.Evidence) error
}

// CheckCurrent orders required checks and isolates values passed to providers.
// It does not send content or implement providers, a distributed snapshot, or
// a send/revoke barrier. Nil means this check sequence passed at its observations.
func CheckCurrent(ctx context.Context, operation CheckOperation, control CurrentControl, reads contracts.QueryReadUnit, backend CurrentBackend, limits CurrentLimits) error {
	if ctx == nil || control == nil || operation.Request.RequestID == "" || !reviewDigest(operation.Request.ProfileDigest) || operation.Request.Deadline.IsZero() || len(operation.Expectation.Published) > 1000 || len(operation.Evidence) > 1000 || operation.Expectation.ScopeVersion != operation.Scope.PolicyVersion {
		return ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalid
	}
	ctx, cancel := context.WithDeadline(ctx, operation.Request.Deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	fixed, err := copyCheckValue(operation)
	if err != nil {
		return err
	}
	if err := bindCheckEvidence(fixed); err != nil {
		return err
	}
	required := make([]contracts.ID, 0)
	documents := make([]contracts.ID, 0, len(fixed.Expectation.Published))
	seen := map[contracts.ID]bool{}
	for _, f := range fixed.Expectation.Published {
		documents = append(documents, f.DocumentID)
		if !seen[f.KnowledgeBaseID] {
			seen[f.KnowledgeBaseID] = true
			required = append(required, f.KnowledgeBaseID)
		}
	}
	refresh := func() (CurrentControlState, error) {
		if err := ctx.Err(); err != nil {
			return CurrentControlState{}, err
		}
		argument, err := copyCheckValue(fixed)
		if err != nil {
			return CurrentControlState{}, err
		}
		state, err := control.Refresh(ctx, argument.Principal, argument.Request)
		if err != nil {
			return CurrentControlState{}, checkFailure(ctx)
		}
		state, err = copyCheckValue(state)
		if err != nil {
			return CurrentControlState{}, err
		}
		if err := MatchRefreshedScope(ctx, fixed.Principal, fixed.Scope, state.Principal, state.Scope, required); err != nil {
			return CurrentControlState{}, err
		}
		if state.ProfileDigest != fixed.Request.ProfileDigest {
			return CurrentControlState{}, ErrStale
		}
		return state, nil
	}
	first, err := refresh()
	if err != nil {
		return err
	}
	if len(documents) > 0 {
		if reads == nil || backend == nil {
			return ErrInvalid
		}
		readScope, err := copyCheckValue(first.Scope)
		if err != nil {
			return err
		}
		observed, err := ObserveCurrent(ctx, reads, readScope, documents, limits)
		if err != nil {
			return checkFailure(ctx)
		}
		if err := MatchCurrentReview(first.Scope, fixed.Expectation, observed, first.ReviewPolicies); err != nil {
			return err
		}
		backendScope, err := copyCheckValue(first.Scope)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		backendEvidence, err := copyCheckValue(fixed.Evidence)
		if err != nil {
			return err
		}
		if err := backend.Verify(ctx, fixed.Request, backendScope, observed, backendEvidence); err != nil {
			return checkFailure(ctx)
		}
	}
	final, err := refresh()
	if err != nil {
		return err
	}
	if err := MatchRefreshedScope(ctx, first.Principal, first.Scope, final.Principal, final.Scope, required); err != nil {
		return err
	}
	for _, kb := range required {
		if first.ReviewPolicies[kb] != final.ReviewPolicies[kb] {
			return ErrStale
		}
	}
	if !final.Scope.ExpiresAt.After(time.Now()) {
		return ErrStale
	}
	return ctx.Err()
}

func copyCheckValue[T any](value T) (T, error) {
	var copied T
	// encoding/json repairs invalid UTF-8; reject it before cloning so identity
	// and identifier bytes cannot change into a different valid operation.
	if !checkTextEncoding(reflect.ValueOf(value)) {
		return copied, ErrInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4<<20 {
		return copied, ErrInvalid
	}
	if json.Unmarshal(raw, &copied) != nil {
		return copied, ErrInvalid
	}
	return copied, nil
}

// Only the closed, non-recursive check DTOs above are passed to this helper.
func checkTextEncoding(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || checkTextEncoding(v.Elem())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !checkTextEncoding(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !checkTextEncoding(v.Index(i)) {
				return false
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if !checkTextEncoding(iter.Key()) || !checkTextEncoding(iter.Value()) {
				return false
			}
		}
	}
	return true
}

func checkFailure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrCheck
}

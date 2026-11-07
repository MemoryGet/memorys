package contracts

import (
	"context"
	"errors"
)

var (
	ErrGrantUnauthenticated = errors.New("grant authentication required")
	ErrGrantDenied          = errors.New("grant authorization denied")
	ErrGrantConflict        = errors.New("grant policy conflict")
	ErrGrantIdempotency     = errors.New("grant idempotency conflict")
	ErrGrantUnavailable     = errors.New("grant management unavailable")
)

type SetGrantInput struct {
	Credential            Credential
	Request               RequestContext
	KnowledgeBaseID       ID
	SubjectID             ID
	Role                  string
	ExpectedPolicyVersion uint64
	IdempotencyKey        string
}

type GrantResult struct {
	KnowledgeBaseID ID
	SubjectID       ID
	Role            string
	PolicyVersion   uint64
}

type GrantApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	SetGrant(context.Context, SetGrantInput) (GrantResult, error)
}

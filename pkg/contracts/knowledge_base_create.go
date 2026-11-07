package contracts

import (
	"context"
	"errors"
)

var (
	ErrKnowledgeBaseCreateUnauthenticated = errors.New("knowledge base create authentication required")
	ErrKnowledgeBaseCreateDenied          = errors.New("knowledge base create authorization denied")
	ErrKnowledgeBaseCreateConflict        = errors.New("knowledge base create idempotency conflict")
)

type KnowledgeBaseCreateInput struct {
	Credential     Credential
	Request        RequestContext
	SpaceID        ID
	Name           string
	IdempotencyKey string
}

type KnowledgeBaseCreateApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	CreateKnowledgeBase(context.Context, KnowledgeBaseCreateInput) (KnowledgeBase, error)
}

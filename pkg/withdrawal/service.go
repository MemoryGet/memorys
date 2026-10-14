// Package withdrawal coordinates only the withdrawal slice. It deliberately
// does not implement the other methods of contracts.PublicationService.
package withdrawal

import (
	"context"
	"errors"
	"math"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrRejected = errors.New("withdrawal rejected or unavailable")

// Prepare must verify identity/delegation and current resource authority, freeze
// semantic digests/config, and generate platform IDs. Never bind it to client DTOs.
// Preparer supplies canonical commitments; production authority remains required.
type Preparation struct {
	OperationID, HistoryID, PlanID, EventID, AuditID contracts.ID
	ActorSemanticDigest, CommandDigest               contracts.Digest
	OccurredAt                                       time.Time
}
type Service struct {
	Transactions contracts.WithdrawalUnit
	Prepare      func(context.Context, contracts.WithdrawRequest) (Preparation, error)
}

func (s Service) Withdraw(ctx context.Context, request contracts.WithdrawRequest) (contracts.WithdrawResult, error) {
	var result contracts.WithdrawResult
	if ctx == nil || s.Transactions == nil || s.Prepare == nil {
		return result, ErrRejected
	}
	request = cloneRequest(request)
	command := request.Command
	if _, ok := ctx.Deadline(); !ok {
		return result, ErrRejected
	}
	if command.Scope.Action != "publication.withdraw" || command.Scope.SubjectID != command.Principal.SubjectID || len(command.Scope.KnowledgeBaseIDs) != 1 || command.Request.RequestID == "" || command.Request.Deadline.IsZero() || request.ExpectedEpoch == 0 || request.ExpectedEpoch == math.MaxUint64 || request.DocumentID == "" || command.IdempotencyKey == "" || request.Reason == "" || !utf8.ValidString(request.Reason) || utf8.RuneCountInString(request.Reason) > 10000 {
		return result, ErrRejected
	}
	deadline := command.Request.Deadline
	if command.Scope.ExpiresAt.Before(deadline) {
		deadline = command.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if ctx.Err() != nil {
		return result, ErrRejected
	}
	prepared, err := s.Prepare(ctx, cloneRequest(request))
	if err != nil {
		return result, err
	}
	if ctx.Err() != nil {
		return result, ErrRejected
	}
	err = s.Transactions.WithinWithdrawal(ctx, command.Scope, func(c context.Context, ports contracts.WithdrawalPorts) error {
		if ports.Reserve == nil || ports.LoadCurrent == nil || ports.Complete == nil || ports.History == nil || ports.Plans == nil {
			return ErrRejected
		}
		reservation, err := ports.Reserve(c, command.Scope, contracts.WithdrawalCommand{OperationID: prepared.OperationID, DocumentID: request.DocumentID, Key: command.IdempotencyKey, ActorSemanticDigest: prepared.ActorSemanticDigest, CommandDigest: prepared.CommandDigest})
		if err != nil {
			return err
		}
		if !reservation.Created {
			if reservation.HistoryID == "" || reservation.PublicationEpoch != request.ExpectedEpoch+1 || reservation.OperationID == "" {
				return ErrRejected
			}
			result = contracts.WithdrawResult{DocumentID: request.DocumentID, HistoryID: reservation.HistoryID, PublicationEpoch: reservation.PublicationEpoch}
			return nil
		}
		if reservation.OperationID != prepared.OperationID {
			return ErrRejected
		}
		current, err := ports.LoadCurrent(c, command.Scope, request.DocumentID)
		if err != nil {
			return err
		}
		if current.DocumentID != request.DocumentID || current.SpaceID != command.Scope.SpaceID || current.KnowledgeBaseID != command.Scope.KnowledgeBaseIDs[0] || current.PublicationEpoch != request.ExpectedEpoch || current.RowVersion == 0 || current.RowVersion == math.MaxUint64 {
			return ErrRejected
		}
		revision, set := current.RevisionID, current.GenerationSetID
		history := contracts.PublicationHistory{ID: prepared.HistoryID, OperationID: prepared.OperationID, SpaceID: current.SpaceID, KnowledgeBaseID: current.KnowledgeBaseID, DocumentID: current.DocumentID, PreviousEpoch: current.PublicationEpoch, PublicationEpoch: current.PublicationEpoch + 1, Action: "withdraw", PreviousRevisionID: &revision, PreviousGenerationSetID: &set, ActorID: command.Principal.SubjectID, Actor: command.Principal, Reason: request.Reason, OccurredAt: prepared.OccurredAt, CommandDigest: prepared.CommandDigest}
		plan := contracts.CleanupPlan{ID: prepared.PlanID, HistoryID: history.ID, OperationID: history.OperationID, SpaceID: current.SpaceID, KnowledgeBaseID: current.KnowledgeBaseID, DocumentID: current.DocumentID, RevisionID: revision, GenerationSetID: set, PublicationEpoch: current.PublicationEpoch, ProfileDigest: command.Request.ProfileDigest, Targets: current.Targets, CreatedAt: prepared.OccurredAt}
		if contracts.ValidatePublicationHistory(history) != nil || contracts.ValidateWithdrawalCleanup(plan, history) != nil {
			return ErrRejected
		}
		if err := ports.History.Append(c, command.Scope, history); err != nil {
			return err
		}
		if err := ports.Plans.Append(c, command.Scope, plan); err != nil {
			return err
		}
		if err := ports.Complete(c, command.Scope, contracts.WithdrawalCompletion{PlanID: plan.ID, ExpectedRowVersion: current.RowVersion, EventID: prepared.EventID, AuditID: prepared.AuditID, RequestID: command.Request.RequestID}); err != nil {
			return err
		}
		result = contracts.WithdrawResult{DocumentID: current.DocumentID, PublicationEpoch: history.PublicationEpoch, HistoryID: history.ID}
		return nil
	})
	if err != nil {
		return contracts.WithdrawResult{}, err
	}
	return result, nil
}

func cloneRequest(r contracts.WithdrawRequest) contracts.WithdrawRequest {
	r.Command.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), r.Command.Scope.KnowledgeBaseIDs...)
	if r.Command.Principal.Delegation != nil {
		d := *r.Command.Principal.Delegation
		r.Command.Principal.Delegation = &d
	}
	return r
}

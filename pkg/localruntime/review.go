package localruntime

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type ReviewDecisionApplication struct{ runtime *Search }

func (s *Search) NewReviewDecisionApplication() (ReviewDecisionApplication, error) {
	if s == nil || s.db == nil || s.auth == nil {
		return ReviewDecisionApplication{}, errors.New("review application unavailable")
	}
	return ReviewDecisionApplication{runtime: s}, nil
}

func (a ReviewDecisionApplication) ProfileIdentity() (uint64, contracts.Digest, error) {
	if a.runtime == nil {
		return 0, "", errors.New("review application unavailable")
	}
	return a.runtime.ProfileIdentity()
}

func (a ReviewDecisionApplication) DecideReview(ctx context.Context, input contracts.ReviewDecisionInput) (contracts.Approval, error) {
	if a.runtime == nil || ctx == nil || input.RevisionID == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.IdempotencyKey == "" {
		return contracts.Approval{}, errors.New("review unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := a.runtime.auth.Authenticate(ctx, secret)
	if err != nil {
		return contracts.Approval{}, errors.New("review unauthenticated")
	}
	_, expectedProfile, err := a.runtime.ProfileIdentity()
	if err != nil || expectedProfile != input.Request.ProfileDigest {
		return contracts.Approval{}, errors.New("review profile changed")
	}
	var documentID, spaceID, knowledgeBaseID string
	if err := a.runtime.db.QueryRowContext(ctx, `SELECT document_id,space_id,kb_id FROM akos_withdraw_v1.revisions WHERE id=$1::text`, string(input.RevisionID)).Scan(&documentID, &spaceID, &knowledgeBaseID); err != nil {
		return contracts.Approval{}, errors.New("review target unavailable")
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "revision.approve", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return contracts.Approval{}, errors.New("review denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, authErr := a.runtime.auth.Authenticate(c, credential)
		if authErr != nil {
			return errors.New("review denied")
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("review denied")
		}
		return nil
	}
	var policyVersionText string
	if err := a.runtime.db.QueryRowContext(ctx, `SELECT policy_version::text FROM akos_withdraw_v1.review_policy_heads WHERE space_id=$1::text AND kb_id=$2::text`, spaceID, knowledgeBaseID).Scan(&policyVersionText); err != nil {
		return contracts.Approval{}, errors.New("review policy unavailable")
	}
	policyVersion, err := strconv.ParseUint(policyVersionText, 10, 64)
	if err != nil || policyVersion == 0 {
		return contracts.Approval{}, errors.New("review policy unavailable")
	}
	command := document.ReviewCommand{DocumentID: contracts.ID(documentID), Request: contracts.ApprovalDecisionRequest{Command: contracts.CommandContext{Request: input.Request, Principal: who, Scope: scope, IdempotencyKey: input.IdempotencyKey}, RevisionID: input.RevisionID, Decision: input.Decision, Reason: input.Reason, ExpectedApprovalVersion: input.ExpectedApprovalVersion}}
	service := postgres.Reviews{DB: a.runtime.db, ProfileDigest: input.Request.ProfileDigest, ReviewPolicyVersion: policyVersion, Verify: func(c context.Context, before contracts.AccessScope) error { return verify(c, before) }, AuthorizeDecision: func(c context.Context, received document.ReviewCommand) error {
		if received.DocumentID != command.DocumentID || received.Request.RevisionID != command.Request.RevisionID || received.Request.Decision != command.Request.Decision || received.Request.ExpectedApprovalVersion != command.Request.ExpectedApprovalVersion {
			return errors.New("review command changed")
		}
		return verify(c, received.Request.Command.Scope)
	}}
	return service.DecideReview(ctx, command)
}

var _ contracts.ReviewDecisionApplication = ReviewDecisionApplication{}

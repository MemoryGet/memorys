package localruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var roleActions = map[string][]string{
	"Viewer":      {"knowledge.search", "knowledge.read"},
	"Contributor": {"knowledge.search", "knowledge.read", "document.create", "document.submit", "document.parse", "original.write"},
	"Admin":       {"knowledge.search", "knowledge.read", "document.create", "document.submit", "document.parse", "original.write", "original.read", "revision.approve", "publication.start", "publication.publish", "publication.withdraw", "document.delete", "publication.cleanup.read", "kb.manage_feedback", "kb.manage_access"},
}

func validGrantRole(role string) bool {
	return role == "none" || roleActions[role] != nil
}

func grantDigest(input contracts.SetGrantInput, actor contracts.Principal, space contracts.ID) (string, error) {
	raw, err := json.Marshal(struct {
		Space, KnowledgeBase, Subject, Role, Issuer, Client, Actor, Key string
		Expected                                                        uint64
	}{string(space), string(input.KnowledgeBaseID), string(input.SubjectID), input.Role, actor.Issuer, string(actor.ClientID), string(actor.SubjectID), input.IdempotencyKey, input.ExpectedPolicyVersion})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(append([]byte("akos.grant.set/1\n"), raw...))
	return hex.EncodeToString(h[:]), nil
}

func grantReceiptID(space, actor, key string) contracts.ID {
	h := sha256.Sum256([]byte(space + "\x00" + actor + "\x00" + key))
	return contracts.ID("grant-receipt-" + hex.EncodeToString(h[:]))
}

func (s *Search) SetGrant(ctx context.Context, input contracts.SetGrantInput) (contracts.GrantResult, error) {
	var empty contracts.GrantResult
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.KnowledgeBaseID == "" || input.SubjectID == "" || !validGrantRole(input.Role) || input.ExpectedPolicyVersion == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || strings.ContainsAny(input.IdempotencyKey, "\x00\r\n") {
		return empty, contracts.ErrGrantUnavailable
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, contracts.ErrGrantUnauthenticated
	}
	version, digest, err := s.ProfileIdentity()
	if err != nil || version == 0 || digest != input.Request.ProfileDigest {
		return empty, contracts.ErrGrantUnavailable
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	space, _, err := s.grantSpace(ctx, authorizer, who, input.KnowledgeBaseID)
	if err != nil {
		return empty, contracts.ErrGrantDenied
	}
	digestText, err := grantDigest(input, who, space)
	if err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	targetIssuer := who.Issuer
	targetClient := who.ClientID
	var enabled bool
	var subjectType string
	if err := s.db.QueryRowContext(ctx, `SELECT s.enabled,s.subject_type FROM akos_withdraw_v1.identity_subjects s WHERE s.issuer=$1 AND s.subject_id=$2`, targetIssuer, string(input.SubjectID)).Scan(&enabled, &subjectType); err != nil || !enabled || subjectType != "user" && subjectType != "service" {
		return empty, contracts.ErrGrantUnavailable
	}
	if err := s.db.QueryRowContext(ctx, `SELECT enabled FROM akos_withdraw_v1.identity_clients WHERE issuer=$1 AND client_id=$2`, targetIssuer, string(targetClient)).Scan(&enabled); err != nil || !enabled {
		return empty, contracts.ErrGrantUnavailable
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	defer tx.Rollback()
	receiptID := grantReceiptID(string(space), string(who.SubjectID), input.IdempotencyKey)
	var oldDigest string
	var oldReceipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_digest,receipt FROM akos_withdraw_v1.access_policy_change_receipts WHERE space_id=$1 AND actor_id=$2 AND command_key=$3 FOR UPDATE`, string(space), string(who.SubjectID), input.IdempotencyKey).Scan(&oldDigest, &oldReceipt)
	if err == nil {
		if oldDigest != digestText {
			return empty, contracts.ErrGrantIdempotency
		}
		var result contracts.GrantResult
		if json.Unmarshal(oldReceipt, &result) != nil || result.KnowledgeBaseID != input.KnowledgeBaseID || result.SubjectID != input.SubjectID {
			return empty, contracts.ErrGrantUnavailable
		}
		if tx.Commit() != nil {
			return empty, contracts.ErrGrantUnavailable
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, contracts.ErrGrantUnavailable
	}
	var currentText string
	if err := tx.QueryRowContext(ctx, `SELECT version::text FROM akos_withdraw_v1.access_policy_heads WHERE space_id=$1 FOR UPDATE`, string(space)).Scan(&currentText); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	current, err := strconv.ParseUint(currentText, 10, 64)
	if err != nil || current != input.ExpectedPolicyVersion || current == ^uint64(0) {
		return empty, contracts.ErrGrantConflict
	}
	next := current + 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots(space_id,version,sealed) VALUES($1,$2,false)`, string(space), next); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action) SELECT space_id,$2,issuer,subject_id,client_id,kb_id,action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=$3 AND NOT (issuer=$4 AND subject_id=$5 AND client_id=$6 AND kb_id=$7)`, string(space), next, current, targetIssuer, string(input.SubjectID), string(targetClient), string(input.KnowledgeBaseID)); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	for _, action := range roleActions[input.Role] {
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action) VALUES($1,$2,$3,$4,$5,$6,$7)`, string(space), next, targetIssuer, string(input.SubjectID), string(targetClient), string(input.KnowledgeBaseID), action); err != nil {
			return empty, contracts.ErrGrantUnavailable
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=$2`, string(space), next); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	if result, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=$2 WHERE space_id=$1 AND version=$3`, string(space), next, current); err != nil {
		return empty, contracts.ErrGrantUnavailable
	} else if rows, _ := result.RowsAffected(); rows != 1 {
		return empty, contracts.ErrGrantConflict
	}
	grant := contracts.GrantResult{KnowledgeBaseID: input.KnowledgeBaseID, SubjectID: input.SubjectID, Role: input.Role, PolicyVersion: next}
	receipt, err := json.Marshal(grant)
	if err != nil || len(receipt) > 65536 {
		return empty, contracts.ErrGrantUnavailable
	}
	payload, _ := json.Marshal(map[string]any{"receipt_id": receiptID, "kb_id": input.KnowledgeBaseID, "subject_id": input.SubjectID, "role": input.Role, "policy_version": next})
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_change_receipts(id,space_id,kb_id,actor_id,command_key,request_digest,receipt) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, string(receiptID), string(space), string(input.KnowledgeBaseID), string(who.SubjectID), input.IdempotencyKey, digestText, string(receipt)); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_change_audit(id,receipt_id,space_id,kb_id,actor_id,subject_id,action,policy_version,payload) VALUES($1,$2,$3,$4,$5,$6,'grant.set',$7,$8::jsonb)`, string(receiptID), string(receiptID), string(space), string(input.KnowledgeBaseID), string(who.SubjectID), string(input.SubjectID), next, string(payload)); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_change_outbox(id,receipt_id,space_id,event_type,payload) VALUES($1,$2,$3,'grant.changed',$4::jsonb)`, string(receiptID), string(receiptID), string(space), string(payload)); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	if err = tx.Commit(); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	return grant, nil
}

func (s *Search) grantSpace(ctx context.Context, authorizer postgres.Authorizer, who contracts.Principal, kb contracts.ID) (contracts.ID, contracts.AccessScope, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT space_id FROM akos_withdraw_v1.access_policy_heads WHERE space_id IN (SELECT space_id FROM akos_withdraw_v1.access_policy_grants WHERE kb_id=$1) ORDER BY space_id LIMIT 100`, string(kb))
	if err != nil {
		return "", contracts.AccessScope{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var space string
		if rows.Scan(&space) != nil {
			continue
		}
		scope, authErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_access", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{kb}})
		if authErr == nil {
			return contracts.ID(space), scope, nil
		}
	}
	return "", contracts.AccessScope{}, contracts.ErrGrantDenied
}

var _ contracts.GrantApplication = (*Search)(nil)

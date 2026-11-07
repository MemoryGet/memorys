package localruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func createKnowledgeBaseID(space, actor, key string) contracts.ID {
	sum := sha256.Sum256([]byte("knowledge-base-create\x00" + space + "\x00" + actor + "\x00" + key))
	return contracts.ID("kb-" + hex.EncodeToString(sum[:]))
}

func createKnowledgeBaseDigest(input contracts.KnowledgeBaseCreateInput, actor contracts.Principal) string {
	sum := sha256.Sum256([]byte(string(input.SpaceID) + "\x00" + input.Name + "\x00" + string(actor.SubjectID) + "\x00" + input.IdempotencyKey))
	return hex.EncodeToString(sum[:])
}

// CreateKnowledgeBase creates a KB and all current configuration heads in one
// serializable transaction. Existing administrators are identified by the
// current revision.approve grant; the resulting KB receives the creator's
// Admin action set in the next sealed ACL snapshot.
func (s *Search) CreateKnowledgeBase(ctx context.Context, input contracts.KnowledgeBaseCreateInput) (contracts.KnowledgeBase, error) {
	var empty contracts.KnowledgeBase
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.SpaceID == "" || strings.TrimSpace(input.Name) == "" || len(input.Name) > 10000 || strings.ContainsAny(input.Name, "\x00\r\n") || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 {
		return empty, errors.New("knowledge base create unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, contracts.ErrKnowledgeBaseCreateUnauthenticated
	}
	version, profileDigest, err := s.ProfileIdentity()
	if err != nil || version == 0 || profileDigest != input.Request.ProfileDigest {
		return empty, errors.New("knowledge base profile changed")
	}
	var aclVersion string
	if err := s.db.QueryRowContext(ctx, `SELECT h.version::text FROM akos_withdraw_v1.access_policy_heads h JOIN akos_withdraw_v1.access_policy_grants g ON g.space_id=h.space_id AND g.version=h.version WHERE h.space_id=$1 AND g.issuer=$2 AND g.subject_id=$3 AND g.client_id=$4 AND g.action='revision.approve'`, string(input.SpaceID), who.Issuer, string(who.SubjectID), string(who.ClientID)).Scan(&aclVersion); err != nil {
		return empty, contracts.ErrKnowledgeBaseCreateDenied
	}
	profile, err := configprofile.Decode(s.profile)
	if err != nil {
		return empty, errors.New("knowledge base profile unavailable")
	}
	policy := document.ReviewPolicySnapshot{Version: 1, AcceptedDecisionPolicyVersions: []uint64{1}}
	policy.Digest, err = document.ReviewPolicyDigest(policy)
	if err != nil {
		return empty, errors.New("knowledge base policy unavailable")
	}
	kbID := createKnowledgeBaseID(string(input.SpaceID), string(who.SubjectID), input.IdempotencyKey)
	// The digest-derived epoch is deterministic; use a stable hash directly
	// instead of relying on a random backend identity.
	epochSum := sha256.Sum256([]byte("backend\x00" + string(kbID) + "\x00" + string(profile.Index.Digest)))
	epochID := contracts.ID("backend-" + hex.EncodeToString(epochSum[:16]))
	backend := document.PublicationBackendSnapshot{EpochID: epochID, Number: 1, CutoverWatermark: "initial-" + string(kbID), IndexProfile: contracts.ProfileReference{ID: profile.Index.ID, Version: profile.Index.Version}, ProfileDigest: profile.Index.Digest, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}}
	backendBytes, _ := json.Marshal(backend)
	policyBytes, _ := json.Marshal(policy)
	kb := contracts.KnowledgeBase{ID: kbID, SpaceID: input.SpaceID, Name: strings.TrimSpace(input.Name), ActiveBackendEpochID: &epochID, RowVersion: 1, PolicyVersion: 1}
	receiptBytes, _ := json.Marshal(kb)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, errors.New("knowledge base create unavailable")
	}
	defer tx.Rollback()
	digestText := createKnowledgeBaseDigest(input, who)
	receiptSum := sha256.Sum256([]byte(string(input.SpaceID) + "\x00" + string(who.SubjectID) + "\x00" + input.IdempotencyKey))
	receiptID := contracts.ID("kb-receipt-" + hex.EncodeToString(receiptSum[:]))
	var oldDigest string
	var oldReceipt []byte
	if err = tx.QueryRowContext(ctx, `SELECT request_digest,receipt FROM akos_withdraw_v1.knowledge_base_create_receipts WHERE space_id=$1 AND actor_id=$2 AND command_key=$3 FOR UPDATE`, string(input.SpaceID), string(who.SubjectID), input.IdempotencyKey).Scan(&oldDigest, &oldReceipt); err == nil {
		if oldDigest != digestText {
			return empty, contracts.ErrKnowledgeBaseCreateConflict
		}
		if json.Unmarshal(oldReceipt, &kb) != nil || tx.Commit() != nil {
			return empty, errors.New("knowledge base create unavailable")
		}
		return kb, nil
	}
	if err != sql.ErrNoRows {
		return empty, errors.New("knowledge base create unavailable")
	}
	var current uint64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM akos_withdraw_v1.access_policy_heads WHERE space_id=$1 FOR UPDATE`, string(input.SpaceID)).Scan(&current); err != nil {
		return empty, contracts.ErrKnowledgeBaseCreateDenied
	}
	if current != parseUint64(aclVersion) || current == ^uint64(0) {
		return empty, contracts.ErrKnowledgeBaseCreateConflict
	}
	next := current + 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots(space_id,version,sealed) VALUES($1,$2,false)`, string(input.SpaceID), next); err != nil {
		return empty, errors.New("knowledge base ACL unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action) SELECT space_id,$2,issuer,subject_id,client_id,kb_id,action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=$3`, string(input.SpaceID), next, current); err != nil {
		return empty, errors.New("knowledge base ACL unavailable")
	}
	for _, action := range roleActions["Admin"] {
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action) VALUES($1,$2,$3,$4,$5,$6,$7)`, string(input.SpaceID), next, who.Issuer, string(who.SubjectID), string(who.ClientID), string(kbID), action); err != nil {
			return empty, errors.New("knowledge base ACL unavailable")
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=$2`, string(input.SpaceID), next); err != nil {
		return empty, errors.New("knowledge base ACL unavailable")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=$2 WHERE space_id=$1 AND version=$3`, string(input.SpaceID), next, current); err != nil {
		return empty, errors.New("knowledge base ACL unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.knowledge_base_metadata(space_id,kb_id,name) VALUES($1,$2,$3)`, string(input.SpaceID), string(kbID), kb.Name); err != nil {
		return empty, errors.New("knowledge base metadata unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.review_policy_snapshots(space_id,kb_id,version,digest,payload) VALUES($1,$2,1,$3,$4::jsonb)`, string(input.SpaceID), string(kbID), string(policy.Digest), string(policyBytes)); err != nil {
		return empty, errors.New("knowledge base policy unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.review_policy_heads(space_id,kb_id,policy_version,row_version) VALUES($1,$2,1,1)`, string(input.SpaceID), string(kbID)); err != nil {
		return empty, errors.New("knowledge base policy unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_backend_epochs(space_id,kb_id,id,payload) VALUES($1,$2,$3,$4::jsonb)`, string(input.SpaceID), string(kbID), string(epochID), string(backendBytes)); err != nil {
		return empty, errors.New("knowledge base backend unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_backend_heads(space_id,kb_id,backend_epoch_id,row_version) VALUES($1,$2,$3,1)`, string(input.SpaceID), string(kbID), string(epochID)); err != nil {
		return empty, errors.New("knowledge base backend unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_catalog(profile_digest,payload) VALUES($1,$2) ON CONFLICT(profile_digest) DO NOTHING`, string(profile.Pipeline.Digest), []byte(s.profile)); err != nil {
		return empty, errors.New("knowledge base query profile unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_bindings(space_id,kb_id,version,profile_digest) VALUES($1,$2,1,$3)`, string(input.SpaceID), string(kbID), string(profile.Pipeline.Digest)); err != nil {
		return empty, errors.New("knowledge base query profile unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_heads(space_id,kb_id,version) VALUES($1,$2,1)`, string(input.SpaceID), string(kbID)); err != nil {
		return empty, errors.New("knowledge base query profile unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.knowledge_base_create_receipts(id,space_id,actor_id,command_key,request_digest,receipt) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, string(receiptID), string(input.SpaceID), string(who.SubjectID), input.IdempotencyKey, digestText, string(receiptBytes)); err != nil {
		return empty, errors.New("knowledge base create unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.knowledge_base_create_audit(id,receipt_id,space_id,kb_id,actor_id,request_id,action,payload) VALUES($1,$2,$3,$4,$5,$6,'knowledge_base.created',$7::jsonb)`, string(receiptID), string(receiptID), string(input.SpaceID), string(kbID), string(who.SubjectID), string(input.Request.RequestID), string(receiptBytes)); err != nil {
		return empty, errors.New("knowledge base create unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.knowledge_base_create_outbox(id,receipt_id,space_id,kb_id,event_type,payload) VALUES($1,$2,$3,$4,'knowledge_base.created',$5::jsonb)`, string(receiptID), string(receiptID), string(input.SpaceID), string(kbID), string(receiptBytes)); err != nil {
		return empty, errors.New("knowledge base create unavailable")
	}
	if tx.Commit() != nil {
		return empty, errors.New("knowledge base create outcome unknown")
	}
	return kb, nil
}

func parseUint64(value string) uint64 {
	var out uint64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + uint64(r-'0')
	}
	return out
}

var _ contracts.KnowledgeBaseCreateApplication = (*Search)(nil)

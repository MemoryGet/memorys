package localcli

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
)

func (s *state) withdraw(ctx context.Context, db *sql.DB, owner *authority, documentID contracts.ID, reason string) (contracts.WithdrawResult, error) {
	var empty contracts.WithdrawResult
	if owner.principal.SubjectType != "user" || owner.principal.SubjectID != s.m.Owner || documentID == "" || reason == "" {
		return empty, errAuthority
	}
	scope, err := owner.scope(ctx, "publication.withdraw", documentID)
	if err != nil {
		return empty, errAuthority
	}
	var epoch string
	if err := db.QueryRowContext(ctx, `SELECT publication_epoch::text FROM akos_withdraw_v1.documents WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND NOT deleted AND active_revision_id IS NOT NULL`, s.m.Space, s.m.KB, documentID).Scan(&epoch); err != nil {
		return empty, errors.New("文档当前没有可撤回的已发布版本")
	}
	expected, err := strconv.ParseUint(epoch, 10, 64)
	if err != nil || expected == 0 {
		return empty, errors.New("文档当前没有可撤回的已发布版本")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return empty, errAuthority
	}
	command := contracts.CommandContext{Request: contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: s.m.ProfileDigest, Deadline: deadline}, Principal: owner.principal, Scope: scope, IdempotencyKey: "local-" + digest([]byte(string(s.m.Installation)+"/withdraw/"+string(documentID)+"/"+strconv.FormatUint(expected, 10)+"/"+reason))}
	preparer := withdrawal.Preparer{Verify: func(c context.Context, request contracts.WithdrawRequest) error {
		if request.DocumentID != documentID || request.Command.Principal.SubjectID != owner.principal.SubjectID {
			return errAuthority
		}
		return owner.verify(c, request.Command.Scope)
	}, NewID: func(context.Context) (contracts.ID, error) { return contracts.ID("local-withdraw-" + nonce()), nil }, Now: func() time.Time { return time.Now().UTC() }}
	service := withdrawal.Service{Transactions: postgres.WithdrawalAdapter{DB: db, Verify: owner.verify}, Prepare: preparer.Prepare}
	result, err := service.Withdraw(ctx, contracts.WithdrawRequest{Command: command, DocumentID: documentID, ExpectedEpoch: expected, Reason: reason})
	if err != nil {
		return empty, errors.New("撤回失败，请检查当前文档状态后重试")
	}
	return result, nil
}

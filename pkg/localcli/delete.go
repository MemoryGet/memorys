package localcli

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// deleteDocument performs a logical document delete. PostgreSQL creates the
// immutable delete history and a cleanup job; physical index/blob cleanup is
// intentionally left to the existing cleanup worker contract.
func (s *state) deleteDocument(ctx context.Context, db *sql.DB, owner *authority, documentID contracts.ID) (contracts.DeleteDocumentResult, error) {
	var empty contracts.DeleteDocumentResult
	if owner == nil || owner.principal.SubjectType != "user" || owner.principal.SubjectID != s.m.Owner || documentID == "" {
		return empty, errAuthority
	}
	if _, err := owner.scope(ctx, "document.delete", documentID); err != nil {
		return empty, errAuthority
	}
	var rawVersion string
	var deleted bool
	if err := db.QueryRowContext(ctx, `SELECT row_version::text,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1 AND kb_id=$2 AND id=$3`, s.m.Space, s.m.KB, documentID).Scan(&rawVersion, &deleted); err != nil || deleted {
		return empty, errors.New("文档不存在或已经删除")
	}
	version, err := strconv.ParseUint(rawVersion, 10, 64)
	if err != nil || version == 0 {
		return empty, errDatabase
	}
	key := "local-" + digest([]byte(string(s.m.Installation)+"/delete/"+string(documentID)+"/"+rawVersion))
	command, err := owner.command(ctx, "document.delete", documentID, key)
	if err != nil {
		return empty, errAuthority
	}
	valid := func(c context.Context, q contracts.DeleteDocumentRequest) error {
		if q.DocumentID != documentID || q.ExpectedDocumentVersion != version || q.Command.Scope.Action != "document.delete" {
			return errAuthority
		}
		return owner.checkCommand(c, q.Command, "document.delete", documentID)
	}
	store := postgres.DocumentDeletions{
		DB: db,
		Verify: func(c context.Context, scope contracts.AccessScope) error {
			return owner.verify(c, scope)
		},
		AuthorizeDelete: valid,
	}
	service := document.DeletionService{Store: store, Verify: valid}
	result, err := service.Delete(ctx, contracts.DeleteDocumentRequest{Command: command, DocumentID: documentID, ExpectedDocumentVersion: version})
	if err != nil {
		return empty, errors.New("删除文档失败，索引清理任务未完成")
	}
	return result, nil
}

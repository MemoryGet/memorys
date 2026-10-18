package postgres

import (
	"context"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// DeletionAuthority resolves ownership even after logical deletion so an
// authorized retry can recover its receipt. No read grant is requested.
// The permission-to-domain-action mapping is platform-owned and rechecked by
// the bound application control at every transaction authorization boundary.
type DeletionAuthority struct{ Authorizer Authorizer }

func (p DeletionAuthority) AuthorizeDocumentDeletion(ctx context.Context, who contracts.Principal, document contracts.ID) (contracts.DeletionTarget, error) {
	var empty contracts.DeletionTarget
	if ctx == nil || p.Authorizer.DB == nil || !commandID(string(document)) {
		return empty, ErrAuthorization
	}
	var space, kb contracts.ID
	if p.Authorizer.DB.QueryRowContext(ctx, `SELECT space_id,kb_id FROM akos_withdraw_v1.documents WHERE id=$1`, document).Scan(&space, &kb) != nil {
		return empty, ErrAuthorization
	}
	grant, e := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.delete", SpaceID: space, KnowledgeBaseIDs: []contracts.ID{kb}, ResourceIDs: []contracts.ID{document}})
	if e != nil || grant.Action != "kb.delete" || grant.SubjectID != who.SubjectID || grant.SpaceID != space || len(grant.KnowledgeBaseIDs) != 1 || grant.KnowledgeBaseIDs[0] != kb || !grant.ExpiresAt.After(time.Now()) {
		return empty, ErrAuthorization
	}
	grant.Action = "document.delete"
	return contracts.DeletionTarget{DocumentID: document, SpaceID: space, KnowledgeBaseID: kb, Scope: grant}, nil
}

var _ contracts.DeletionAuthority = DeletionAuthority{}

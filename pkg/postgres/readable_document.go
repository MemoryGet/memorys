package postgres

import (
	"context"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var _ contracts.ReadableDocumentLocator = Authorizer{}

// LocateReadableDocument does not accept caller-selected ownership. The join
// discloses a location only under an explicit current read grant. Authorize then
// opens a fresh policy observation and checks the document's resource ownership.
// The caller must authenticate Principal first and recheck before content output.
func (p Authorizer) LocateReadableDocument(ctx context.Context, who contracts.Principal, document contracts.ID) (contracts.ReadableDocument, error) {
	var empty contracts.ReadableDocument
	if ctx == nil || p.DB == nil || ctx.Err() != nil || !commandID(string(document)) || !commandID(string(who.SubjectID)) || !commandID(string(who.ClientID)) || who.Issuer == "" || len(who.Issuer) > 2048 || !utf8.ValidString(who.Issuer) || (who.SubjectType != "user" && who.SubjectType != "service") || who.Delegation != nil {
		return empty, ErrAuthorization
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrAuthorization
	}
	var space, kb contracts.ID
	err := p.DB.QueryRowContext(ctx, `SELECT d.space_id,d.kb_id FROM akos_withdraw_v1.documents d
 JOIN akos_withdraw_v1.access_policy_heads h ON h.space_id=d.space_id
 JOIN akos_withdraw_v1.access_policy_grants g ON g.space_id=h.space_id AND g.version=h.version AND g.kb_id=d.kb_id AND g.action='knowledge.read'
 JOIN akos_withdraw_v1.identity_issuers i ON i.issuer=g.issuer AND i.enabled
 JOIN akos_withdraw_v1.identity_subjects s ON s.issuer=g.issuer AND s.subject_id=g.subject_id AND s.enabled
 JOIN akos_withdraw_v1.identity_clients c ON c.issuer=g.issuer AND c.client_id=g.client_id AND c.enabled
 WHERE d.id=$1 AND NOT d.deleted AND d.active_revision_id IS NOT NULL AND g.issuer=$2 AND g.subject_id=$3 AND s.subject_type=$4 AND g.client_id=$5`, document, who.Issuer, who.SubjectID, who.SubjectType, who.ClientID).Scan(&space, &kb)
	if err != nil || !commandID(string(space)) || !commandID(string(kb)) {
		return empty, ErrAuthorization
	}
	scope, err := p.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.read", SpaceID: space, KnowledgeBaseIDs: []contracts.ID{kb}, ResourceIDs: []contracts.ID{document}})
	if err != nil || ctx.Err() != nil {
		return empty, ErrAuthorization
	}
	return contracts.ReadableDocument{DocumentID: document, SpaceID: space, KnowledgeBaseID: kb, Scope: scope}, nil
}

package postgres

import (
	"context"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Stream actual frozen bodies within the existing view. consume is private,
// synchronous byte verification only; it must not issue SQL or external I/O.
func (r *queryRepository) frozenRevisions(ctx context.Context, facts []contracts.PublishedFact, consume func(contracts.PublishedFact, []byte) error) error {
	byID := map[contracts.ID]contracts.PublishedFact{}
	docs, revisions := []contracts.ID{}, []contracts.ID{}
	for _, fact := range facts {
		if byID[fact.DocumentID].DocumentID != "" {
			return ErrRepository
		}
		byID[fact.DocumentID] = fact
		docs = append(docs, fact.DocumentID)
		revisions = append(revisions, fact.RevisionID)
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT kb_id,document_id,revision_id,payload,frozen_payload FROM akos_withdraw_v1.revision_contents
 WHERE space_id=$1::text AND kb_id=ANY($2::text[]) AND document_id=ANY($3::text[]) AND revision_id=ANY($4::text[]) LIMIT 1001`,
		string(r.scope.SpaceID), queryKBs(r.scope.KnowledgeBaseIDs), queryKBs(docs), queryKBs(revisions))
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	seen := map[contracts.ID]bool{}
	for rows.Next() {
		var kb, doc, revision contracts.ID
		var raw, payload []byte
		if rows.Scan(&kb, &doc, &revision, &raw, &payload) != nil {
			return ErrRepository
		}
		fact, ok := byID[doc]
		state, loaded := r.states[doc]
		if !ok || !loaded || seen[doc] || fact.KnowledgeBaseID != kb || fact.RevisionID != revision || state.PublicationReview == nil {
			return ErrRepository
		}
		seen[doc] = true
		_, verified, err := frozenRevisionBytes(raw, payload, state.PublicationReview.Revision)
		if err != nil {
			return err
		}
		if err = consume(fact, verified); err != nil {
			return err
		}
	}
	if rows.Err() != nil || len(seen) != len(facts) {
		return ErrRepository
	}
	return nil
}

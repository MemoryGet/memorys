package localcli

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// answerFilter is applied before semantic ranking and answer generation. It
// deliberately mirrors the local evidence filters so the answer cannot cite a
// source that the user excluded in the workbench.
type answerFilter struct {
	Source        string
	Status        string
	Tags          []string
	UpdatedAfter  time.Time
	UpdatedBefore time.Time
}

func parseAnswerFilter(source, status, updatedAfter, updatedBefore string, rawTags []string) (answerFilter, error) {
	if source != "" && source != "original" && source != "revision" {
		return answerFilter{}, errors.New("来源筛选只能是 original 或 revision。")
	}
	if status != "" && status != "published" && status != "historical" {
		return answerFilter{}, errors.New("发布状态筛选只能是 published 或 historical。")
	}
	tags, err := normalizeTags(rawTags)
	if err != nil {
		return answerFilter{}, err
	}
	filter := answerFilter{Source: source, Status: status, Tags: tags}
	if updatedAfter != "" {
		filter.UpdatedAfter, err = time.Parse(time.RFC3339, updatedAfter)
		if err != nil {
			return answerFilter{}, errors.New("更新时间起点必须是 RFC3339 时间。")
		}
	}
	if updatedBefore != "" {
		filter.UpdatedBefore, err = time.Parse(time.RFC3339, updatedBefore)
		if err != nil {
			return answerFilter{}, errors.New("更新时间终点必须是 RFC3339 时间。")
		}
	}
	if !filter.UpdatedAfter.IsZero() && !filter.UpdatedBefore.IsZero() && !filter.UpdatedAfter.Before(filter.UpdatedBefore) {
		return answerFilter{}, errors.New("更新时间范围无效。")
	}
	return filter, nil
}

func answerFilterHasValues(filter answerFilter) bool {
	return filter.Source != "" || filter.Status != "" || len(filter.Tags) > 0 || !filter.UpdatedAfter.IsZero() || !filter.UpdatedBefore.IsZero()
}

type answerSourceFact struct {
	deleted bool
	active  sql.NullString
	updated sql.NullTime
}

type answerSourceFactKey struct {
	document contracts.ID
	revision contracts.ID
}

func (s *state) filterAnswerCandidates(ctx context.Context, db *sql.DB, candidates []contracts.Candidate, filter answerFilter) ([]contracts.Candidate, error) {
	if s == nil || db == nil {
		return nil, errState
	}
	facts := map[answerSourceFactKey]answerSourceFact{}
	out := make([]contracts.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if filter.Source != "" && string(candidate.Citation.Source.Kind) != filter.Source {
			continue
		}
		documentID := candidate.Citation.DocumentID
		key := answerSourceFactKey{document: documentID, revision: candidate.Citation.RevisionID}
		fact, ok := facts[key]
		if !ok {
			var err error
			err = db.QueryRowContext(ctx, `SELECT d.deleted,d.active_revision_id,max(h.occurred_at) FROM akos_withdraw_v1.documents d LEFT JOIN akos_withdraw_v1.publication_history h ON h.space_id=d.space_id AND h.kb_id=d.kb_id AND h.document_id=d.id AND h.revision_id=$4 AND h.action='publish' WHERE d.space_id=$1 AND d.kb_id=$2 AND d.id=$3 GROUP BY d.deleted,d.active_revision_id`, s.m.Space, s.m.KB, documentID, candidate.Citation.RevisionID).Scan(&fact.deleted, &fact.active, &fact.updated)
			if err != nil {
				return nil, errState
			}
			facts[key] = fact
		}
		status := "historical"
		if !fact.deleted && fact.active.Valid && fact.active.String == string(candidate.Citation.RevisionID) {
			status = "published"
		}
		if filter.Status != "" && status != filter.Status {
			continue
		}
		if !filter.UpdatedAfter.IsZero() && (!fact.updated.Valid || fact.updated.Time.Before(filter.UpdatedAfter)) || !filter.UpdatedBefore.IsZero() && (!fact.updated.Valid || !fact.updated.Time.Before(filter.UpdatedBefore)) {
			continue
		}
		if len(filter.Tags) > 0 && !tagsMatch(s.m.Tags[documentID], filter.Tags) {
			continue
		}
		out = append(out, candidate)
	}
	return out, nil
}

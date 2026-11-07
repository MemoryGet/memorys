package localcli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type localFeedback struct {
	ID              contracts.ID `json:"feedback_id"`
	Query           string       `json:"query,omitempty"`
	DocumentID      contracts.ID `json:"document_id"`
	RevisionID      contracts.ID `json:"revision_id"`
	BlockID         contracts.ID `json:"block_id"`
	Kind            string       `json:"kind"`
	Comment         string       `json:"comment"`
	CreatedAt       time.Time    `json:"created_at"`
	State           string       `json:"state"`
	Resolution      string       `json:"resolution,omitempty"`
	ResolvedAt      *time.Time   `json:"resolved_at,omitempty"`
	ProposalDraftID contracts.ID `json:"proposal_draft_id,omitempty"`
}

type feedbackPublication struct {
	RevisionID  contracts.ID `json:"revision_id"`
	HistoryID   contracts.ID `json:"history_id"`
	PublishedAt time.Time    `json:"published_at"`
}

type feedbackListRow struct {
	localFeedback
	Publication *feedbackPublication `json:"proposal_publication,omitempty"`
}

func (h *webHost) feedbackPublications(ctx context.Context, records []localFeedback) ([]feedbackListRow, error) {
	result := make([]feedbackListRow, len(records))
	type proposalKey struct {
		DocumentID contracts.ID `json:"document_id"`
		DraftID    contracts.ID `json:"draft_id"`
	}
	positions := make(map[proposalKey][]int)
	keys := make([]proposalKey, 0)
	for i, record := range records {
		result[i].localFeedback = record
		if record.ProposalDraftID == "" {
			continue
		}
		key := proposalKey{record.DocumentID, record.ProposalDraftID}
		if len(positions[key]) == 0 {
			keys = append(keys, key)
		}
		positions[key] = append(positions[key], i)
	}
	if len(keys) == 0 {
		return result, nil
	}
	payload, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT r.document_id,r.draft_id,r.revision_id,p.id,p.occurred_at
FROM jsonb_to_recordset($3::jsonb) AS k(document_id text,draft_id text)
JOIN akos_withdraw_v1.submission_receipts r
  ON r.space_id=$1 AND r.kb_id=$2 AND r.document_id=k.document_id AND r.draft_id=k.draft_id
JOIN LATERAL (
  SELECT id,occurred_at FROM akos_withdraw_v1.publication_history
  WHERE space_id=r.space_id AND kb_id=r.kb_id AND document_id=r.document_id
    AND revision_id=r.revision_id AND action='publish'
  ORDER BY publication_epoch DESC LIMIT 1
) p ON true`, h.s.m.Space, h.s.m.KB, string(payload))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key proposalKey
		var publication feedbackPublication
		var occurredAt string
		if err := rows.Scan(&key.DocumentID, &key.DraftID, &publication.RevisionID, &publication.HistoryID, &occurredAt); err != nil {
			return nil, err
		}
		publication.PublishedAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, err
		}
		for _, i := range positions[key] {
			result[i].Publication = &publication
		}
	}
	return result, rows.Err()
}

func (f *localFeedback) normalize() {
	if f.State == "" {
		f.State = "open"
	}
}

func (h *webHost) feedbackList(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	documentID := strings.TrimSpace(r.URL.Query().Get("document_id"))
	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	tags, tagErr := documentTagFilter(r)
	if tagErr != nil {
		return tagErr
	}
	filtered, filterErr := filterFeedback(records, state, kind, documentID)
	if filterErr != nil {
		return filterErr
	}
	filtered, filterErr = filterFeedbackRevision(filtered, revisionID)
	if filterErr != nil {
		return filterErr
	}
	filtered = filterFeedbackTags(filtered, tags, h.s.m.Tags)
	from, to, err := feedbackTimeBounds(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		return err
	}
	filtered = filterFeedbackTime(filtered, from, to)
	summary := map[string]int{"helpful": 0, "incorrect": 0, "missing": 0, "outdated": 0, "other": 0}
	states := map[string]int{"open": 0, "resolved": 0, "dismissed": 0}
	for _, record := range records {
		record.normalize()
		summary[record.Kind]++
		states[record.State]++
	}
	judged := summary["helpful"] + summary["incorrect"] + summary["missing"] + summary["outdated"]
	quality := map[string]any{"judged": judged, "helpful_rate": float64(0), "issue_rate": float64(0), "open": states["open"]}
	if judged > 0 {
		quality["helpful_rate"] = float64(summary["helpful"]) / float64(judged)
		quality["issue_rate"] = float64(summary["incorrect"]+summary["missing"]+summary["outdated"]) / float64(judged)
	}
	list, err := h.feedbackPublications(ctx, filtered)
	if err != nil {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"feedback": list, "count": len(list), "summary": summary, "states": states, "quality": quality})
}

func filterFeedbackTime(records []localFeedback, from, to time.Time) []localFeedback {
	if from.IsZero() && to.IsZero() {
		return records
	}
	filtered := make([]localFeedback, 0, len(records))
	for _, record := range records {
		if !from.IsZero() && record.CreatedAt.Before(from) || !to.IsZero() && !record.CreatedAt.Before(to) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

func (h *webHost) evaluationReport(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	documentID := strings.TrimSpace(r.URL.Query().Get("document_id"))
	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	tags, tagErr := documentTagFilter(r)
	if tagErr != nil {
		return tagErr
	}
	filtered, err := filterFeedback(records, state, kind, documentID)
	if err != nil {
		return err
	}
	filtered, err = filterFeedbackRevision(filtered, revisionID)
	if err != nil {
		return err
	}
	filtered = filterFeedbackTags(filtered, tags, h.s.m.Tags)
	from, to, err := feedbackTimeBounds(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		return err
	}
	judged, helpful, issues, queries, docs, revisions := 0, 0, 0, map[string]struct{}{}, map[contracts.ID]struct{}{}, map[contracts.ID]struct{}{}
	filtered = filterFeedbackTime(filtered, from, to)
	for _, record := range filtered {
		record.normalize()
		if record.Query != "" {
			queries[record.Query] = struct{}{}
		}
		docs[record.DocumentID] = struct{}{}
		revisions[record.RevisionID] = struct{}{}
		switch record.Kind {
		case "helpful":
			judged++
			helpful++
		case "incorrect", "missing", "outdated":
			judged++
			issues++
		}
	}
	report := map[string]any{"format": "memorys.retrieval-evaluation/1", "samples": len(filtered), "judged": judged, "helpful_rate": float64(0), "issue_rate": float64(0), "unique_queries": len(queries), "unique_documents": len(docs), "unique_revisions": len(revisions)}
	if judged > 0 {
		report["helpful_rate"] = float64(helpful) / float64(judged)
		report["issue_rate"] = float64(issues) / float64(judged)
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, report)
}

func feedbackTimeBounds(fromRaw, toRaw string) (time.Time, time.Time, error) {
	var from, to time.Time
	var err error
	if strings.TrimSpace(fromRaw) != "" {
		from, err = time.Parse(time.RFC3339, strings.TrimSpace(fromRaw))
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("无效的评测起始时间。")
		}
	}
	if strings.TrimSpace(toRaw) != "" {
		to, err = time.Parse(time.RFC3339, strings.TrimSpace(toRaw))
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("无效的评测结束时间。")
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New("评测时间范围无效。")
	}
	return from, to, nil
}

func (h *webHost) exportFeedback(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	documentID := strings.TrimSpace(r.URL.Query().Get("document_id"))
	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	tags, tagErr := documentTagFilter(r)
	if tagErr != nil {
		return tagErr
	}
	filtered, err := filterFeedback(records, state, kind, documentID)
	if err != nil {
		return err
	}
	filtered, err = filterFeedbackRevision(filtered, revisionID)
	if err != nil {
		return err
	}
	filtered = filterFeedbackTags(filtered, tags, h.s.m.Tags)
	from, to, err := feedbackTimeBounds(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		return err
	}
	filtered = filterFeedbackTime(filtered, from, to)
	if len(filtered) > 10000 {
		return errors.New("反馈导出超出上限。")
	}
	raw, err := json.Marshal(map[string]any{"format": "memorys.feedback-export/1", "count": len(filtered), "feedback": filtered})
	if err != nil || len(raw) > 8<<20 {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="memorys-feedback.json"`)
	_, err = w.Write(raw)
	return err
}

func filterFeedback(records []localFeedback, state, kind, documentID string) ([]localFeedback, error) {
	if state != "" && state != "open" && state != "resolved" && state != "dismissed" {
		return nil, errors.New("无效的反馈状态。")
	}
	if kind != "" && !validFeedbackKind(kind) {
		return nil, errors.New("无效的反馈类型。")
	}
	if len(documentID) > 200 || strings.ContainsAny(documentID, "\r\n\x00") {
		return nil, errors.New("无效的文档标识。")
	}
	filtered := make([]localFeedback, 0, len(records))
	for _, record := range records {
		record.normalize()
		if state != "" && record.State != state || kind != "" && record.Kind != kind || documentID != "" && record.DocumentID != contracts.ID(documentID) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered, nil
}

func filterFeedbackRevision(records []localFeedback, revisionID string) ([]localFeedback, error) {
	if len(revisionID) > 200 || strings.ContainsAny(revisionID, "\r\n\x00") {
		return nil, errors.New("无效的版本标识。")
	}
	if revisionID == "" {
		return records, nil
	}
	filtered := make([]localFeedback, 0, len(records))
	for _, record := range records {
		if record.RevisionID == contracts.ID(revisionID) {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func filterFeedbackTags(records []localFeedback, tags []string, tagMap map[contracts.ID][]string) []localFeedback {
	if len(tags) == 0 {
		return records
	}
	filtered := make([]localFeedback, 0, len(records))
	for _, record := range records {
		if tagsMatch(tagMap[record.DocumentID], tags) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func (h *webHost) feedback(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		Query      string       `json:"query"`
		DocumentID contracts.ID `json:"document_id"`
		RevisionID contracts.ID `json:"revision_id"`
		BlockID    contracts.ID `json:"block_id"`
		Kind       string       `json:"kind"`
		Comment    string       `json:"comment"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DocumentID == "" || q.RevisionID == "" || q.BlockID == "" || !validFeedbackKind(q.Kind) || !validFeedbackText(q.Comment) || !validFeedbackQuery(q.Query) {
		return errors.New("请提供有效的引用反馈。")
	}
	scope, err := a.scope(ctx, "knowledge.search", q.DocumentID)
	if err != nil {
		return errAuthority
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	record := localFeedback{ID: contracts.ID("feedback-" + digest([]byte(string(h.s.m.Installation)+"\x00"+string(q.DocumentID)+"\x00"+string(q.RevisionID)+"\x00"+string(q.BlockID)+"\x00"+q.Kind+"\x00"+q.Comment+"\x00"+q.Query+"\x00"+nonce()))), Query: strings.TrimSpace(q.Query), DocumentID: q.DocumentID, RevisionID: q.RevisionID, BlockID: q.BlockID, Kind: q.Kind, Comment: q.Comment, CreatedAt: time.Now().UTC(), State: "open"}
	records = append(records, record)
	if len(records) > 10000 {
		records = records[len(records)-10000:]
	}
	raw, err := json.Marshal(records)
	if err != nil || len(raw) > 8<<20 || writeAtomic(h.s.root, "feedback.json", raw) != nil {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"feedback_id": record.ID, "state": "received"})
}

func (h *webHost) resolveFeedback(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		FeedbackID contracts.ID `json:"feedback_id"`
		State      string       `json:"state"`
		Resolution string       `json:"resolution"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.FeedbackID == "" || (q.State != "resolved" && q.State != "dismissed") || !validFeedbackText(q.Resolution) {
		return errors.New("请提供有效的反馈处理结果。")
	}
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	now := time.Now().UTC()
	found := false
	for i := range records {
		records[i].normalize()
		if records[i].ID != q.FeedbackID {
			continue
		}
		records[i].State, records[i].Resolution, records[i].ResolvedAt = q.State, strings.TrimSpace(q.Resolution), &now
		found = true
		break
	}
	if !found {
		return errors.New("反馈不存在或已被清理。")
	}
	raw, err := json.Marshal(records)
	if err != nil || len(raw) > 8<<20 || writeAtomic(h.s.root, "feedback.json", raw) != nil {
		return errDatabase
	}
	return webJSON(w, map[string]any{"feedback_id": q.FeedbackID, "state": q.State})
}

func validFeedbackKind(value string) bool {
	switch value {
	case "helpful", "incorrect", "missing", "outdated", "other":
		return true
	default:
		return false
	}
}

func validFeedbackText(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 10000
}

func validFeedbackQuery(value string) bool {
	return utf8.ValidString(value) && len(value) <= 4096 && !strings.ContainsRune(value, 0)
}

func readFeedback(root string) ([]localFeedback, error) {
	f, err := os.Open(filepath.Join(root, "feedback.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []localFeedback{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return nil, errors.New("feedback store unavailable")
	}
	if len(raw) == 0 {
		return []localFeedback{}, nil
	}
	var records []localFeedback
	if json.Unmarshal(raw, &records) != nil || len(records) > 10000 {
		return nil, errors.New("feedback store unavailable")
	}
	for i := range records {
		records[i].normalize()
	}
	return records, nil
}

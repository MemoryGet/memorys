package localcli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

const (
	maxLocalEvaluationSets  = 100
	maxLocalEvaluationRows  = 10000
	maxEvaluationSetTitle   = 120
	maxEvaluationSetPayload = 8 << 20
)

type localEvaluationSet struct {
	ID         contracts.ID    `json:"evaluation_set_id"`
	Title      string          `json:"title"`
	CreatedAt  time.Time       `json:"created_at"`
	State      string          `json:"state,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	DocumentID contracts.ID    `json:"document_id,omitempty"`
	RevisionID contracts.ID    `json:"revision_id,omitempty"`
	Tags       []string        `json:"tags,omitempty"`
	From       *time.Time      `json:"from,omitempty"`
	To         *time.Time      `json:"to,omitempty"`
	Samples    []localFeedback `json:"samples"`
}

func evaluationSetPath(root string) string { return filepath.Join(root, "evaluation_sets.json") }

func readEvaluationSets(root string) ([]localEvaluationSet, error) {
	raw, err := os.ReadFile(evaluationSetPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return []localEvaluationSet{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err != nil || len(raw) > maxEvaluationSetPayload {
		return nil, errors.New("评测集存储不可用。")
	}
	if len(raw) == 0 {
		return []localEvaluationSet{}, nil
	}
	var sets []localEvaluationSet
	if json.Unmarshal(raw, &sets) != nil || len(sets) > maxLocalEvaluationSets {
		return nil, errors.New("评测集存储不可用。")
	}
	for i := range sets {
		if sets[i].ID == "" || sets[i].CreatedAt.IsZero() || len(sets[i].Samples) > maxLocalEvaluationRows {
			return nil, errors.New("评测集存储不可用。")
		}
	}
	return sets, nil
}

func saveEvaluationSets(root string, sets []localEvaluationSet) error {
	if len(sets) > maxLocalEvaluationSets {
		return errors.New("评测集数量超出上限。")
	}
	raw, err := json.Marshal(sets)
	if err != nil || len(raw) > maxEvaluationSetPayload || writeAtomic(root, "evaluation_sets.json", raw) != nil {
		return errDatabase
	}
	return nil
}

func selectEvaluationFeedback(records []localFeedback, state, kind string, documentID, revisionID string, tags []string, tagMap map[contracts.ID][]string, from, to time.Time) ([]localFeedback, error) {
	filtered, err := filterFeedback(records, state, kind, documentID)
	if err != nil {
		return nil, err
	}
	filtered, err = filterFeedbackRevision(filtered, revisionID)
	if err != nil {
		return nil, err
	}
	filtered = filterFeedbackTags(filtered, tags, tagMap)
	return filterFeedbackTime(filtered, from, to), nil
}

func validEvaluationSetTitle(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maxEvaluationSetTitle && !strings.ContainsAny(value, "\x00\r\n")
}

func (h *webHost) createEvaluationSet(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		Title      string       `json:"title"`
		State      string       `json:"state"`
		Kind       string       `json:"kind"`
		DocumentID contracts.ID `json:"document_id"`
		RevisionID contracts.ID `json:"revision_id"`
		Tags       []string     `json:"tags"`
		From       string       `json:"from"`
		To         string       `json:"to"`
		Limit      int          `json:"limit"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if !validEvaluationSetTitle(q.Title) {
		return errors.New("请提供有效的评测集名称。")
	}
	tags, err := normalizeTags(q.Tags)
	if err != nil {
		return err
	}
	from, to, err := feedbackTimeBounds(q.From, q.To)
	if err != nil {
		return err
	}
	if q.Limit == 0 {
		q.Limit = 1000
	}
	if q.Limit < 1 || q.Limit > maxLocalEvaluationRows {
		return errors.New("评测集样本数量必须在 1-10000 之间。")
	}
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	samples, err := selectEvaluationFeedback(records, strings.TrimSpace(q.State), strings.TrimSpace(q.Kind), strings.TrimSpace(string(q.DocumentID)), strings.TrimSpace(string(q.RevisionID)), tags, h.s.m.Tags, from, to)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return errors.New("当前筛选没有可保存的反馈样本。")
	}
	if len(samples) > q.Limit {
		samples = samples[len(samples)-q.Limit:]
	}
	now := time.Now().UTC()
	set := localEvaluationSet{ID: contracts.ID("evalset-" + digest([]byte(string(h.s.m.Installation)+"\x00"+now.Format(time.RFC3339Nano)+"\x00"+q.Title))), Title: strings.TrimSpace(q.Title), CreatedAt: now, State: strings.TrimSpace(q.State), Kind: strings.TrimSpace(q.Kind), DocumentID: contracts.ID(strings.TrimSpace(string(q.DocumentID))), RevisionID: contracts.ID(strings.TrimSpace(string(q.RevisionID))), Tags: append([]string(nil), tags...), Samples: append([]localFeedback(nil), samples...)}
	if !from.IsZero() {
		value := from
		set.From = &value
	}
	if !to.IsZero() {
		value := to
		set.To = &value
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	sets, err := readEvaluationSets(h.s.root)
	if err != nil {
		return errDatabase
	}
	sets = append(sets, set)
	if len(sets) > maxLocalEvaluationSets {
		sets = sets[len(sets)-maxLocalEvaluationSets:]
	}
	if err := saveEvaluationSets(h.s.root, sets); err != nil {
		return err
	}
	return webJSON(w, map[string]any{"evaluation_set_id": set.ID, "title": set.Title, "created_at": set.CreatedAt, "samples": len(set.Samples)})
}

func (h *webHost) evaluationSets(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	sets, err := readEvaluationSets(h.s.root)
	if err != nil {
		return errDatabase
	}
	items := make([]map[string]any, 0, len(sets))
	for _, set := range sets {
		items = append(items, map[string]any{"evaluation_set_id": set.ID, "title": set.Title, "created_at": set.CreatedAt, "samples": len(set.Samples), "state": set.State, "kind": set.Kind, "document_id": set.DocumentID, "revision_id": set.RevisionID, "tags": set.Tags})
	}
	return webJSON(w, map[string]any{"sets": items})
}

func (h *webHost) evaluationSet(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	id := contracts.ID(r.PathValue("id"))
	if id == "" || len(id) > 200 || strings.ContainsAny(string(id), "\r\n\x00") {
		return errors.New("评测集标识无效。")
	}
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	sets, err := readEvaluationSets(h.s.root)
	if err != nil {
		return errDatabase
	}
	for _, set := range sets {
		if set.ID == id {
			return webJSON(w, set)
		}
	}
	return errors.New("评测集不存在。")
}

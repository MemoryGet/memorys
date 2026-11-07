package localcli

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func normalizeTags(input []string) ([]string, error) {
	if len(input) > 20 {
		return nil, errors.New("标签最多 20 个。")
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(input))
	total := 0
	for _, raw := range input {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" || !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > 40 || strings.ContainsAny(tag, "\r\n\x00") {
			return nil, errors.New("标签不能为空、不能超过 40 个字符或包含控制字符。")
		}
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
			total += len(tag)
		}
	}
	if total > 512 {
		return nil, errors.New("标签总长度不能超过 512 字节。")
	}
	return result, nil
}

func mergeTags(existing, add, remove []string) ([]string, error) {
	current := make(map[string]struct{}, len(existing)+len(add))
	for _, tag := range existing {
		current[tag] = struct{}{}
	}
	for _, tag := range add {
		current[tag] = struct{}{}
	}
	for _, tag := range remove {
		delete(current, tag)
	}
	merged := make([]string, 0, len(current))
	for tag := range current {
		merged = append(merged, tag)
	}
	sort.Strings(merged)
	return normalizeTags(merged)
}

func (h *webHost) setTags(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DocumentID contracts.ID `json:"document_id"`
		Tags       []string     `json:"tags"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.DocumentID == "" {
		return errors.New("请提供文档标识和标签。")
	}
	tags, err := normalizeTags(q.Tags)
	if err != nil {
		return err
	}
	scope, err := a.scope(ctx, "document.create", q.DocumentID)
	if err != nil {
		return errAuthority
	}
	var exists bool
	if err := h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.documents WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND NOT deleted)`, h.s.m.Space, h.s.m.KB, q.DocumentID).Scan(&exists); err != nil || !exists {
		return errors.New("文档不存在或已删除。")
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	if h.s.m.Tags == nil {
		h.s.m.Tags = map[contracts.ID][]string{}
	}
	if len(tags) == 0 {
		delete(h.s.m.Tags, q.DocumentID)
	} else {
		h.s.m.Tags[q.DocumentID] = append([]string(nil), tags...)
	}
	if err := writeAtomic(h.s.root, "manifest.json", encode(h.s.m)); err != nil {
		return errState
	}
	return webJSON(w, map[string]any{"document_id": q.DocumentID, "tags": tags})
}

// tagsList returns counts for tags attached to live documents in this
// knowledge base. The manifest is local state, while the database query keeps
// deleted documents from surfacing as reusable suggestions.
func (h *webHost) tagsList(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM akos_withdraw_v1.documents WHERE space_id=$1 AND kb_id=$2 AND NOT deleted`, h.s.m.Space, h.s.m.KB)
	if err != nil {
		return errDatabase
	}
	live := map[contracts.ID]struct{}{}
	for rows.Next() {
		var id contracts.ID
		if rows.Scan(&id) != nil {
			rows.Close()
			return errDatabase
		}
		live[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errDatabase
	}
	rows.Close()
	h.mutations.Lock()
	counts := map[string]int{}
	for documentID, tags := range h.s.m.Tags {
		if _, ok := live[documentID]; !ok {
			continue
		}
		for _, tag := range tags {
			counts[tag]++
		}
	}
	h.mutations.Unlock()
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	type tagCount struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	result := make([]tagCount, 0, len(counts))
	for tag, count := range counts {
		result = append(result, tagCount{Tag: tag, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Tag < result[j].Tag
	})
	return webJSON(w, map[string]any{"tags": result})
}

// batchTags updates a bounded set of documents in one manifest write. Each
// document is authorized independently before the in-memory snapshot is
// swapped, so a failed target cannot leave a partially updated batch.
func (h *webHost) batchTags(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DocumentIDs []contracts.ID `json:"document_ids"`
		Add         []string       `json:"add"`
		Remove      []string       `json:"remove"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if a == nil || h == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	if len(q.DocumentIDs) == 0 || len(q.DocumentIDs) > 100 {
		return errors.New("批量标签操作需要 1-100 个文档。")
	}
	add, err := normalizeTags(q.Add)
	if err != nil {
		return err
	}
	remove, err := normalizeTags(q.Remove)
	if err != nil {
		return err
	}
	removed := make(map[string]struct{}, len(remove))
	for _, tag := range remove {
		removed[tag] = struct{}{}
	}
	for _, tag := range add {
		if _, ok := removed[tag]; ok {
			return errors.New("同一个标签不能同时添加和移除。")
		}
	}
	seen := make(map[contracts.ID]struct{}, len(q.DocumentIDs))
	scopes := make([]contracts.AccessScope, 0, len(q.DocumentIDs))
	for _, documentID := range q.DocumentIDs {
		if documentID == "" {
			return errors.New("文档标识不能为空。")
		}
		if _, ok := seen[documentID]; ok {
			return errors.New("文档列表不能包含重复标识。")
		}
		seen[documentID] = struct{}{}
		scope, scopeErr := a.scope(ctx, "document.create", documentID)
		if scopeErr != nil {
			return errAuthority
		}
		var exists bool
		if dbErr := h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.documents WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND NOT deleted)`, h.s.m.Space, h.s.m.KB, documentID).Scan(&exists); dbErr != nil || !exists {
			return errors.New("文档不存在或已删除。")
		}
		scopes = append(scopes, scope)
	}
	if len(add) == 0 && len(remove) == 0 {
		return errors.New("请至少提供一个要添加或移除的标签。")
	}
	for _, scope := range scopes {
		if a.verify(ctx, scope) != nil {
			return errAuthority
		}
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	next := make(map[contracts.ID][]string, len(h.s.m.Tags)+len(q.DocumentIDs))
	for documentID, tags := range h.s.m.Tags {
		next[documentID] = append([]string(nil), tags...)
	}
	updated := make([]map[string]any, 0, len(q.DocumentIDs))
	for _, documentID := range q.DocumentIDs {
		finalTags, normErr := mergeTags(next[documentID], add, remove)
		if normErr != nil {
			return normErr
		}
		if len(finalTags) == 0 {
			delete(next, documentID)
		} else {
			next[documentID] = finalTags
		}
		updated = append(updated, map[string]any{"document_id": documentID, "tags": finalTags})
	}
	if err := writeAtomic(h.s.root, "manifest.json", encode(func() *manifest {
		copy := h.s.m
		copy.Tags = next
		return &copy
	}())); err != nil {
		return errState
	}
	h.s.m.Tags = next
	return webJSON(w, map[string]any{"updated": updated, "count": len(updated)})
}

func (h *webHost) tagsFor(documentID contracts.ID) []string {
	if h.s.m.Tags == nil {
		return nil
	}
	return append([]string(nil), h.s.m.Tags[documentID]...)
}

func tagsMatch(actual, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(actual))
	for _, tag := range actual {
		set[tag] = struct{}{}
	}
	for _, tag := range required {
		if _, ok := set[tag]; !ok {
			return false
		}
	}
	return true
}

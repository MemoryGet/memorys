package localcli

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type recallQuery struct {
	Text     string
	Relevant map[string]struct{}
}

type recallSummary struct {
	Evaluated int
	Hits      int
	MRR       float64
}

func citationDocumentKey(documentID, revisionID contracts.ID) string {
	return string(documentID) + "\x00" + string(revisionID)
}

func calculateRecallAtK(queries []recallQuery, results map[string][]contracts.Candidate, k int) recallSummary {
	if k < 1 {
		return recallSummary{}
	}
	var summary recallSummary
	for _, query := range queries {
		if len(query.Relevant) == 0 {
			continue
		}
		summary.Evaluated++
		first := 0
		for rank, candidate := range results[query.Text] {
			if rank >= k {
				break
			}
			if _, ok := query.Relevant[citationDocumentKey(candidate.Citation.DocumentID, candidate.Citation.RevisionID)]; !ok {
				continue
			}
			if first == 0 {
				first = rank + 1
			}
		}
		if first > 0 {
			summary.Hits++
			summary.MRR += 1 / float64(first)
		}
	}
	if summary.Evaluated > 0 {
		summary.MRR /= float64(summary.Evaluated)
	}
	return summary
}

func (h *webHost) evaluationRecall(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	id := contracts.ID(r.PathValue("id"))
	if id == "" || len(id) > 200 || strings.ContainsAny(string(id), "\r\n\x00") {
		return errors.New("评测集标识无效。")
	}
	k := 10
	if raw := r.URL.Query().Get("k"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 50 || len(r.URL.Query()["k"]) != 1 {
			return errors.New("Recall@K 的 K 必须在 1-50 之间。")
		}
		k = value
	}
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	sets, err := readEvaluationSets(h.s.root)
	if err != nil {
		return errDatabase
	}
	var set *localEvaluationSet
	for i := range sets {
		if sets[i].ID == id {
			set = &sets[i]
			break
		}
	}
	if set == nil {
		return errors.New("评测集不存在。")
	}
	queries := make([]recallQuery, 0, 100)
	byText := map[string]*recallQuery{}
	skipped := 0
	for _, sample := range set.Samples {
		text := strings.TrimSpace(sample.Query)
		if text == "" || sample.Kind != "helpful" {
			if text == "" {
				skipped++
			}
			continue
		}
		item := byText[text]
		if item == nil {
			if len(queries) >= 100 {
				break
			}
			queries = append(queries, recallQuery{Text: text, Relevant: map[string]struct{}{}})
			item = &queries[len(queries)-1]
			byText[text] = item
		}
		item.Relevant[citationDocumentKey(sample.DocumentID, sample.RevisionID)] = struct{}{}
	}
	if len(queries) == 0 {
		return errors.New("评测集没有带查询问题的 helpful 正例。")
	}
	search, err := h.s.searchService(h.db)
	if err != nil {
		return err
	}
	results := make(map[string][]contracts.Candidate, len(queries))
	started := time.Now()
	for _, query := range queries {
		deadline := time.Now().Add(30 * time.Second)
		candidates, searchErr := search.Search(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID("evaluation-" + nonce()), Deadline: deadline, SpaceID: h.s.m.Space, KnowledgeBaseIDs: []contracts.ID{h.s.m.KB}, Text: query.Text, Limit: k})
		if searchErr != nil || ctx.Err() != nil {
			return errors.New("评测检索执行失败，请稍后重试。")
		}
		results[query.Text] = candidates
	}
	summary := calculateRecallAtK(queries, results, k)
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"format": "memorys.retrieval-evaluation/recall/1", "evaluation_set_id": id, "k": k, "samples": len(set.Samples), "evaluated_queries": summary.Evaluated, "hits": summary.Hits, "recall_at_k": ratio(summary.Hits, summary.Evaluated), "mrr": summary.MRR, "skipped_without_query": skipped, "search_latency_ms": float64(time.Since(started).Microseconds()) / 1000})
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

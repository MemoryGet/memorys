package localcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// searchStream exposes only the answer-generation event path. Retrieval and
// generation still run through the same local authority, corpus snapshot and
// final citation checks as the JSON search endpoint.
func (h *webHost) searchStream(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		Query         string   `json:"query"`
		Source        string   `json:"source_kind"`
		Status        string   `json:"source_status"`
		Tags          []string `json:"tags"`
		UpdatedAfter  string   `json:"updated_after"`
		UpdatedBefore string   `json:"updated_before"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if len(q.Query) == 0 || len(q.Query) > 4096 {
		return errors.New("请输入不超过 4096 字节的问题。")
	}
	filter, err := parseAnswerFilter(q.Source, q.Status, q.UpdatedAfter, q.UpdatedBefore, q.Tags)
	if err != nil {
		return err
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("当前响应不支持流式输出。")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(kind string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, raw); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	result, err := h.s.aiWithLimitSinkFiltered(ctx, h.db, a, q.Query, true, 10, filter, func(event contracts.AnswerEvent) error {
		switch event.Kind {
		case "meta":
			return send("meta", map[string]any{"request_id": "stream"})
		case "answer_delta":
			return send("answer_delta", map[string]string{"text": event.Text})
		default:
			return nil
		}
	})
	if err != nil {
		_ = send("error", map[string]string{"message": err.Error()})
		return nil
	}
	response := map[string]any{"mode": "answer", "answer": result["answer"], "status": result["status"], "model": result["answer_model"], "citations": []webCitation{}, "evidence": []webEvidence{}}
	if list, ok := result["evidence"].([]contracts.Evidence); ok {
		evidence := make([]webEvidence, 0, len(list))
		for _, item := range list {
			converted, convertErr := h.webEvidence(ctx, item.Citation, item.SourceExcerpt, item.Rank, nil, nil)
			if convertErr != nil {
				_ = send("error", map[string]string{"message": convertErr.Error()})
				return nil
			}
			evidence = append(evidence, converted)
		}
		response["evidence"] = evidence
	}
	if list, ok := result["citations"].([]contracts.Citation); ok {
		citations := make([]webCitation, 0, len(list))
		for _, item := range list {
			citations = append(citations, webCite(item))
		}
		response["citations"] = citations
	}
	_ = send("done", response)
	return nil
}

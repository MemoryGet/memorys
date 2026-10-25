package modelclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func embeddingSpec() contracts.EmbeddingSpec {
	return contracts.EmbeddingSpec{ProviderModel: "ollama/test:latest", ModelRevision: strings.Repeat("a", 64), Dimension: 16, Distance: "cosine", Normalization: "l2", PreprocessVersion: "utf8-exact/1"}
}
func TestOllamaPinsModelAndRejectsInvalidVectors(t *testing.T) {
	for _, mode := range []string{"valid", "zero", "dimension", "changed"} {
		t.Run(mode, func(t *testing.T) {
			digest := strings.Repeat("a", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "test:latest", "digest": digest}}})
					return
				}
				var input struct {
					Model    string
					Input    []string
					Truncate bool
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Truncate || input.Model != "test:latest" {
					t.Error("embedding request changed")
				}
				vectors := []contracts.Vector{}
				for range input.Input {
					v := make(contracts.Vector, 16)
					v[0] = 2
					if mode == "zero" {
						v[0] = 0
					}
					if mode == "dimension" {
						v = v[:15]
					}
					vectors = append(vectors, v)
				}
				if mode == "changed" {
					digest = strings.Repeat("b", 64)
				}
				json.NewEncoder(w).Encode(map[string]any{"model": "test:latest", "embeddings": vectors})
			}))
			defer server.Close()
			client, err := NewOllama(server.URL, embeddingSpec())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := client.Embed(ctx, contracts.EmbedRequest{Request: contracts.RequestContext{Deadline: time.Now().Add(time.Second)}, Texts: []string{"中文知识"}, Spec: embeddingSpec()})
			if mode == "valid" {
				if err != nil || len(result.Vectors) != 1 || result.Vectors[0][0] != 1 {
					t.Fatal("normalization rejected", err)
				}
			} else if err == nil {
				t.Fatal("unsafe vector accepted")
			}
		})
	}
}
func TestDeepSeekCitationsAndBoundedOutput(t *testing.T) {
	for _, mode := range []string{"valid", "invented", "unmarked", "truncated", "overbudget", "error", "insufficient"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-private-key" {
					t.Error("missing authorization")
				}
				var input map[string]any
				if json.NewDecoder(r.Body).Decode(&input) != nil || input["stream"] != false || input["max_tokens"] != float64(64) || input["tools"] != nil {
					t.Error("generation budget or tools changed")
				}
				if mode == "error" {
					w.WriteHeader(500)
					w.Write([]byte("fixture-private-key private source diagnostic"))
					return
				}
				answer := map[string]any{"answer": "口令是蓝鲸。[1]", "citations": []int{1}, "insufficient_evidence": false}
				if mode == "invented" {
					answer["citations"] = []int{2}
					answer["answer"] = "蓝鲸。[2]"
				}
				if mode == "unmarked" {
					answer["answer"] = "蓝鲸"
				}
				if mode == "insufficient" {
					answer["answer"] = "不足"
					answer["citations"] = []int{}
					answer["insufficient_evidence"] = true
				}
				body, _ := json.Marshal(answer)
				finish, tokens := "stop", 12
				if mode == "truncated" {
					finish = "length"
				}
				if mode == "overbudget" {
					tokens = 65
				}
				json.NewEncoder(w).Encode(map[string]any{"model": "deepseek-flash", "choices": []any{map[string]any{"finish_reason": finish, "message": map[string]any{"content": string(body)}}}, "usage": map[string]int{"completion_tokens": tokens}})
			}))
			defer server.Close()
			client, err := NewDeepSeek("fixture-private-key", "deepseek-flash", 1)
			if err != nil {
				t.Fatal(err)
			}
			client.endpoint = server.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			end := time.Now().Add(time.Second)
			q := contracts.GenerateRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: end}, Scope: contracts.AccessScope{Action: "knowledge.answer", ExpiresAt: end}, Question: "口令？", Evidence: []contracts.Evidence{{Citation: contracts.Citation{DocumentID: "doc"}, SourceExcerpt: "口令是蓝鲸", Rank: 1}}, Budget: contracts.ExecutionBudget{Deadline: end, MaxOutputTokens: 64}}
			var events []contracts.AnswerEvent
			err = client.Generate(ctx, q, func(e contracts.AnswerEvent) error { events = append(events, e); return nil })
			if mode == "valid" || mode == "insufficient" {
				if err != nil || len(events) != 2 || events[1].Kind != "done" {
					t.Fatal("valid response rejected", err)
				}
			} else if err == nil || len(events) != 0 || strings.Contains(err.Error(), "private") {
				t.Fatal("unsafe response escaped", err)
			}
		})
	}
}

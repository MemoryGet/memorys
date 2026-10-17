package contentverify_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestReadContentVerifiesWholeOriginalAndFailsClosed(t *testing.T) {
	for _, mode := range []string{"ok", "revision", "invented-suffix", "geometry", "derived", "table", "empty-text", "foreign-record", "hash", "denied-original", "projection-revoked", "history", "write", "wildcard", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			q, f, r, store, _, control := approvedOriginalFixture(t, ctx, "text/plain")
			q.Scope.Action = "knowledge.read"
			var root map[string]any
			if err := json.Unmarshal(r.record.Payload, &root); err != nil {
				t.Fatal(err)
			}
			block := root["blocks"].([]any)[0].(map[string]any)
			source := block["source_anchor"].(map[string]any)
			switch mode {
			case "revision":
				block["source_anchor"] = map[string]any{"kind": "revision", "revision_block_id": "block", "locator": map[string]any{"precision": "coarse"}}
			case "invented-suffix":
				block["text"] = block["text"].(string) + "人工编造后缀"
			case "geometry":
				source["locator"].(map[string]any)["page"] = 1
			case "derived":
				source["derived_from"] = []any{map[string]any{"blob_id": "other", "locator": map[string]any{"precision": "coarse"}}}
			case "table":
				block["kind"] = "table"
			case "empty-text":
				block["text"] = ""
			case "history":
				q.Scope.Action = "knowledge.history"
			case "write":
				q.Scope.Action = "knowledge.write"
			case "wildcard":
				q.Scope.Action = "knowledge.*"
			case "cancelled":
				cancel()
			}
			raw, _ := json.Marshal(root)
			h, err := canonical.Frozen(raw)
			if err != nil {
				t.Fatal(err)
			}
			r.record.Payload = raw
			f.ContentHash, f.BlocksHash = contracts.Digest(h.ContentHash), contracts.Digest(h.BlocksHash)
			record := r.record
			if mode == "foreign-record" {
				record.DocumentID = "other"
			}
			if mode == "hash" {
				record.Payload = append(append([]byte(nil), record.Payload...), '!')
			}
			base := control
			grants, projections := 0, 0
			control = func(c context.Context, p contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
				grants++
				if mode == "denied-original" {
					return contracts.BlobReadRequest{}, errors.New("private authorization failure")
				}
				return base(c, p, b)
			}
			projector := originalProjector(func(c context.Context, m string, b []byte) (contentverify.OriginalTextProjection, error) {
				projections++
				if mode == "projection-revoked" {
					r.err = errors.New("withdrawn")
				}
				return (contentverify.UTF8OriginalText{}).ProjectOriginalText(c, m, b)
			})
			v := contentverify.TextReadVerifier{Reader: r, Originals: store, Control: control, Projector: projector}
			err = v.VerifyReadContent(ctx, q, f, record)
			good := mode == "ok" || mode == "revision"
			if good && err != nil || !good && err == nil {
				t.Fatalf("unexpected verification result: %v", err)
			}
			if mode == "ok" && (grants < 4 || projections != 1) {
				t.Fatal("original authorization/bytes bypassed")
			}
			if mode == "revision" && (grants != 0 || projections != 0) {
				t.Fatal("revision text misrepresented as original")
			}
			if !good && mode != "denied-original" && mode != "projection-revoked" && mode != "invented-suffix" && grants != 0 {
				t.Fatal("unsupported output reached original storage")
			}
		})
	}
}

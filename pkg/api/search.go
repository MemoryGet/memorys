package api

import (
	"context"
	"encoding/json"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"net/http"
)

// Prepare and Verify are application-service hooks, never client-provided data.
// They must authenticate and prepare fixed publication, then perform current output checks.
type SearchHandler struct {
	Retriever contracts.Retriever
	Prepare   func(*http.Request) (contracts.RetrieveRequest, error)
	Verify    func(context.Context, contracts.RetrieveRequest, []contracts.Candidate) error
}

func (h SearchHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.Retriever == nil || h.Prepare == nil || h.Verify == nil {
		http.Error(w, "search not configured", 503)
		return
	}
	req, err := h.Prepare(r)
	if err != nil {
		http.Error(w, "search denied", 403)
		return
	}
	if req.Request.Deadline.IsZero() {
		http.Error(w, "search unavailable", 503)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), req.Request.Deadline)
	defer cancel()
	results, err := h.Retriever.Search(ctx, req)
	if err != nil {
		http.Error(w, "search failed", 503)
		return
	}
	if err := h.Verify(ctx, req, results); err != nil || ctx.Err() != nil {
		http.Error(w, "search verification failed", 503)
		return
	}
	// Provisional endpoint is not the partner OpenAPI transport. Do not expose provider IDs.
	output := make([]map[string]any, 0, len(results))
	for _, c := range results {
		if c.Citation.DocumentID == "" || c.Citation.RevisionID == "" || c.Citation.BlockID == "" {
			http.Error(w, "invalid evidence", 503)
			return
		}
		output = append(output, map[string]any{"document_id": c.Citation.DocumentID, "revision_id": c.Citation.RevisionID, "block_id": c.Citation.BlockID, "text": c.Text, "rank": c.Rank, "source_anchor": sourceDTO(c.Citation.Source)})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"results": output})
}

func sourceDTO(a contracts.SourceAnchor) map[string]any {
	result := map[string]any{"kind": a.Kind, "locator": locatorDTO(a.Locator)}
	if a.BlobID != "" {
		result["blob_id"] = a.BlobID
	}
	if a.RevisionBlockID != "" {
		result["revision_block_id"] = a.RevisionBlockID
	}
	if len(a.DerivedFrom) > 0 {
		origins := make([]map[string]any, 0, len(a.DerivedFrom))
		for _, o := range a.DerivedFrom {
			origins = append(origins, map[string]any{"blob_id": o.BlobID, "locator": locatorDTO(o.Locator)})
		}
		result["derived_from"] = origins
	}
	return result
}
func locatorDTO(l contracts.Locator) map[string]any {
	result := map[string]any{"precision": l.Precision}
	if l.Page != nil {
		result["page"] = *l.Page
	}
	if l.Slide != nil {
		result["slide"] = *l.Slide
	}
	if l.Worksheet != "" {
		result["worksheet"] = l.Worksheet
	}
	if l.CellRange != "" {
		result["cell_range"] = l.CellRange
	}
	if l.ParagraphPath != nil {
		result["paragraph_path"] = l.ParagraphPath
	}
	if l.Characters != nil {
		result["char_range"] = []int{l.Characters.Start, l.Characters.End}
	}
	if l.Box != nil {
		result["bbox"] = []float64{l.Box.Left, l.Box.Top, l.Box.Right, l.Box.Bottom}
	}
	return result
}

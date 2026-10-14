package modulewire

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type memorySnapshot struct {
	SchemaVersion           string
	ID, OwnerID, SpaceID    contracts.ID
	KnowledgeBaseID, TaskID *contracts.ID
	Content                 string
	Provenance              contracts.MemoryProvenance
	CreatedAt               time.Time
	ExpiresAt               *time.Time
	Version                 uint64
}

func init() {
	register(memorySnapshot{}, "SchemaVersion:schema_version ID:id OwnerID:owner_id SpaceID:space_id KnowledgeBaseID:kb_id? TaskID:task_id? Content:content Provenance:provenance CreatedAt:created_at ExpiresAt:expires_at? Version:version")
	register(contracts.MemoryProvenance{}, "Kind:kind ConfirmationID:confirmation_id? TaskID:task_id? FeedbackID:feedback_id? Citations:citations")
}

func memoryID(id contracts.ID) bool {
	return id != "" && utf8.ValidString(string(id)) && utf8.RuneCountInString(string(id)) <= 200
}

func memoryLocator(l contracts.Locator) bool {
	if l.Precision != "exact" && l.Precision != "coarse" {
		return false
	}
	if (l.Page != nil && *l.Page < 1) || (l.Slide != nil && *l.Slide < 1) {
		return false
	}
	if !utf8.ValidString(l.Worksheet) || !utf8.ValidString(l.CellRange) || (l.CellRange != "" && l.Worksheet == "") {
		return false
	}
	for _, part := range l.ParagraphPath {
		if part < 0 {
			return false
		}
	}
	if l.ParagraphPath != nil && len(l.ParagraphPath) == 0 {
		return false
	}
	if l.Characters != nil && (l.Characters.Start < 0 || l.Characters.End < l.Characters.Start) {
		return false
	}
	if l.Box != nil {
		b := l.Box
		if l.Page == nil || b.Left >= b.Right || b.Top >= b.Bottom {
			return false
		}
		for _, v := range []float64{b.Left, b.Top, b.Right, b.Bottom} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
				return false
			}
		}
	}
	return true
}

func checkMemory(r contracts.MemoryRecord) bool {
	for _, id := range []contracts.ID{r.ID, r.OwnerID, r.SpaceID} {
		if !memoryID(id) {
			return false
		}
	}
	for _, id := range []*contracts.ID{r.KnowledgeBaseID, r.TaskID, r.Provenance.ConfirmationID, r.Provenance.TaskID, r.Provenance.FeedbackID} {
		if id != nil && !memoryID(*id) {
			return false
		}
	}
	if !utf8.ValidString(r.Content) || r.Content == "" || utf8.RuneCountInString(r.Content) > 16000 || r.Version == 0 || r.CreatedAt.IsZero() || contracts.ValidateMemoryProvenance(r.Provenance) != nil {
		return false
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(r.CreatedAt) {
		return false
	}
	if r.Provenance.TaskID != nil && (r.TaskID == nil || *r.TaskID != *r.Provenance.TaskID) {
		return false
	}
	for _, c := range r.Provenance.Citations {
		if !memoryID(c.DocumentID) || !memoryID(c.RevisionID) || !memoryID(c.BlockID) || !memoryLocator(c.Source.Locator) {
			return false
		}
		a := c.Source
		if a.Kind == "original" {
			if !memoryID(a.BlobID) || a.RevisionBlockID != "" {
				return false
			}
		} else if a.Kind == "revision" {
			if !memoryID(a.RevisionBlockID) || a.BlobID != "" {
				return false
			}
		} else {
			return false
		}
		for _, original := range a.DerivedFrom {
			if !memoryID(original.BlobID) || !memoryLocator(original.Locator) {
				return false
			}
		}
	}
	return true
}

// These codecs check offline snapshot shape, not source authenticity, current
// permission, canonical-content bounds, persistence or deletion propagation.
func EncodeMemoryRecord(r contracts.MemoryRecord) ([]byte, error) {
	if !checkMemory(r) {
		return nil, invalid()
	}
	s := memorySnapshot{SchemaVersion: "1.0", ID: r.ID, OwnerID: r.OwnerID, SpaceID: r.SpaceID, KnowledgeBaseID: r.KnowledgeBaseID, TaskID: r.TaskID, Content: r.Content, Provenance: r.Provenance, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, Version: r.Version}
	raw, err := encode(s)
	if err != nil || len(raw) > 4<<20 {
		return nil, invalid()
	}
	return raw, nil
}

func DecodeMemoryRecord(raw []byte) (contracts.MemoryRecord, error) {
	if len(raw) > 4<<20 {
		return contracts.MemoryRecord{}, invalid()
	}
	var s memorySnapshot
	if decode(raw, &s) != nil || s.SchemaVersion != "1.0" {
		return contracts.MemoryRecord{}, invalid()
	}
	// Canonical integer conversion may accept 1.0; the record version requires
	// a raw integer token, matching the Python offline validator.
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var root map[string]any
	if d.Decode(&root) != nil {
		return contracts.MemoryRecord{}, invalid()
	}
	if !memoryOptionalText(root) {
		return contracts.MemoryRecord{}, invalid()
	}
	n, ok := root["version"].(json.Number)
	if !ok || strings.ContainsAny(string(n), ".eE") {
		return contracts.MemoryRecord{}, invalid()
	}
	r := contracts.MemoryRecord{ID: s.ID, OwnerID: s.OwnerID, SpaceID: s.SpaceID, KnowledgeBaseID: s.KnowledgeBaseID, TaskID: s.TaskID, Content: s.Content, Provenance: s.Provenance, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt, Version: s.Version}
	if !checkMemory(r) {
		return contracts.MemoryRecord{}, invalid()
	}
	return r, nil
}

// Optional native strings cannot distinguish absent from explicit empty wire
// members. Validate that distinction before returning the decoded snapshot.
func memoryOptionalText(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "worksheet" || key == "cell_range" {
				text, ok := child.(string)
				if !ok || text == "" {
					return false
				}
			}
			if !memoryOptionalText(child) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !memoryOptionalText(child) {
				return false
			}
		}
	}
	return true
}

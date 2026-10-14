package modulewire_test

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func memoryRecord() contracts.MemoryRecord {
	id := contracts.ID("confirmation")
	now := time.Date(2026, 10, 13, 0, 0, 0, 1, time.UTC)
	end := now.Add(time.Nanosecond)
	return contracts.MemoryRecord{ID: "memory", OwnerID: "user", SpaceID: "space", Content: "偏好", Version: math.MaxUint64, CreatedAt: now, ExpiresAt: &end,
		Provenance: contracts.MemoryProvenance{Kind: contracts.MemoryUserConfirmed, ConfirmationID: &id, Citations: []contracts.Citation{}}}
}

func TestMemoryRecordRoundtripKeepsIntegerAndNanosecond(t *testing.T) {
	r := memoryRecord()
	raw, err := modulewire.EncodeMemoryRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "18446744073709551615") || strings.Contains(string(raw), "kb_id") {
		t.Fatal(string(raw))
	}
	out, err := modulewire.DecodeMemoryRecord(raw)
	if err != nil || !reflect.DeepEqual(out, r) {
		t.Fatal(out, err)
	}
}

func TestMemoryRecordRejectsClosedShapeAndVersionVariants(t *testing.T) {
	raw, err := modulewire.EncodeMemoryRecord(memoryRecord())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), "18446744073709551615", "1.0", 1),
		strings.Replace(string(raw), "18446744073709551615", "1e0", 1),
		strings.Replace(string(raw), "18446744073709551615", "true", 1),
		strings.Replace(string(raw), `"id":"memory"`, `"id":"memory","id":"other"`, 1),
		strings.Replace(string(raw), `"content":"偏好"`, `"content":"偏好","cause":"supplier"`, 1),
		strings.Replace(string(raw), `"owner_id":"user"`, `"owner_id":null`, 1),
		strings.Replace(string(raw), `"schema_version":"1.0"`, `"schema_version":"2.0"`, 1),
	} {
		if _, err := modulewire.DecodeMemoryRecord([]byte(bad)); err == nil {
			t.Fatal("invalid snapshot accepted", bad)
		}
	}
}

func TestMemoryRecordRejectsTaskExpiryAndBounds(t *testing.T) {
	for _, change := range []func(*contracts.MemoryRecord){
		func(r *contracts.MemoryRecord) { *r.ExpiresAt = r.CreatedAt },
		func(r *contracts.MemoryRecord) { r.Content = strings.Repeat("文", 16001) },
		func(r *contracts.MemoryRecord) { r.OwnerID = contracts.ID(strings.Repeat("文", 201)) },
		func(r *contracts.MemoryRecord) {
			id := contracts.ID("task")
			r.Provenance = contracts.MemoryProvenance{Kind: contracts.MemoryTaskExperience, TaskID: &id}
		},
	} {
		r := memoryRecord()
		change(&r)
		if _, err := modulewire.EncodeMemoryRecord(r); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
}

func TestMemoryRecordCitationLocationsRoundtrip(t *testing.T) {
	r := memoryRecord()
	page := 1
	r.Provenance = contracts.MemoryProvenance{Kind: contracts.MemoryKnowledgeSummary, Citations: []contracts.Citation{{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "original", BlobID: "blob", Locator: contracts.Locator{Precision: "exact", Page: &page, Box: &contracts.BoundingBox{Left: 0.1, Top: 0.2, Right: 0.8, Bottom: 0.9}}}}}}
	raw, err := modulewire.EncodeMemoryRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	out, err := modulewire.DecodeMemoryRecord(raw)
	if err != nil || !reflect.DeepEqual(out, r) {
		t.Fatal(out, err)
	}
	r.Provenance.Citations[0].Source.Locator.Box.Right = 0.05
	if _, err := modulewire.EncodeMemoryRecord(r); err == nil {
		t.Fatal("inverted bounding box")
	}
}

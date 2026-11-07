package api

import (
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestLocatorDTOUsesPartnerCharacterAndBoundingBoxFields(t *testing.T) {
	page := 2
	start, end := 4, 12
	box := contracts.BoundingBox{Left: 0.1, Top: 0.2, Right: 0.8, Bottom: 0.9}
	value := locatorDTO(contracts.Locator{Precision: "exact", Page: &page, Characters: &contracts.CharacterRange{Start: start, End: end}, Box: &box})
	if value["char_start"] != start || value["char_end"] != end || value["page"] != page {
		t.Fatalf("character locator fields were not mapped: %#v", value)
	}
	if _, exists := value["char_range"]; exists {
		t.Fatal("legacy char_range leaked into partner DTO")
	}
	if got, ok := value["bbox"].([]float64); !ok || len(got) != 4 || got[2] != box.Right {
		t.Fatalf("bounding box was not mapped: %#v", value["bbox"])
	}
}

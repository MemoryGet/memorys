package queryread

import "github.com/MemoryGet/memorys/pkg/contracts"

// bindCheckEvidence checks ownership against saved publication facts only.
// Actual block existence, locator bounds and excerpt fidelity require the
// independent CurrentBackend verifier using canonical content/original bytes.
func bindCheckEvidence(op CheckOperation) error {
	if len(op.Evidence) > 1000 {
		return ErrInvalid
	}
	facts := make(map[contracts.ID]contracts.PublishedFact, len(op.Expectation.Published))
	for _, fact := range op.Expectation.Published {
		if fact.DocumentID == "" || fact.RevisionID == "" {
			return ErrInvalid
		}
		if _, exists := facts[fact.DocumentID]; exists {
			return ErrInvalid
		}
		facts[fact.DocumentID] = fact
	}
	for _, evidence := range op.Evidence {
		c := evidence.Citation
		fact, present := facts[c.DocumentID]
		if !present || c.RevisionID != fact.RevisionID || !scopeText(string(c.BlockID)) || evidence.Rank < 1 || evidence.SourceExcerpt == "" {
			return ErrInvalid
		}
		a := c.Source
		switch a.Kind {
		case "original":
			if !scopeText(string(a.BlobID)) || a.RevisionBlockID != "" {
				return ErrInvalid
			}
		case "revision":
			if a.RevisionBlockID != c.BlockID || a.BlobID != "" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if len(a.DerivedFrom) > 256 {
			return ErrInvalid
		}
		for _, origin := range a.DerivedFrom {
			if !scopeText(string(origin.BlobID)) {
				return ErrInvalid
			}
		}
	}
	return nil
}

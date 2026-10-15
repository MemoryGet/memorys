package document

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrCandidate = errors.New("parsed draft candidate rejected")

// ParseCandidate retains task input and base version even when a later human
// edit prevents application. It is not an immutable Revision or a DB receipt.
type ParseCandidate struct {
	DraftID         contracts.ID
	ExpectedVersion uint64
	Input           contracts.BlobRef
	ProfileDigest   contracts.Digest
	ParserVersion   string
	Blocks          []contracts.DraftBlock
	Diagnostics     []contracts.Diagnostic
}

type CandidateMerge struct {
	Applied   bool
	Reason    string
	Draft     contracts.Draft
	Candidate ParseCandidate
}

func CandidateFromParse(req contracts.ParseRequest, expectedVersion uint64, result contracts.ParseResult) (ParseCandidate, error) {
	if expectedVersion == 0 || expectedVersion == math.MaxUint64 || req.Input.ID == "" || req.Input.Size <= 0 || result.ParserVersion == "" || len(result.Blocks) == 0 || len(result.Blocks) > 10000 || !candidateDigest(req.Request.ProfileDigest) || !candidateDigest(req.Input.SHA256) {
		return ParseCandidate{}, ErrCandidate
	}
	blocks, err := FromParsedBlocks(req.DraftID, result.Blocks)
	if err != nil {
		return ParseCandidate{}, ErrCandidate
	}
	for index, block := range blocks {
		// The current single-original parser contract cannot invent revision or
		// foreign-attachment provenance. Rich derived outputs need an explicit extension.
		if block.Order != index || block.Content.Source.Kind != "original" || block.Content.Source.BlobID != req.Input.ID || block.Content.Source.RevisionBlockID != "" || len(block.Content.Source.DerivedFrom) != 0 {
			return ParseCandidate{}, ErrCandidate
		}
	}
	candidate := ParseCandidate{DraftID: req.DraftID, ExpectedVersion: expectedVersion, Input: req.Input, ProfileDigest: req.Request.ProfileDigest, ParserVersion: result.ParserVersion, Blocks: blocks, Diagnostics: result.Diagnostics}
	var owned ParseCandidate
	if copyCandidate(candidate, &owned) != nil {
		return ParseCandidate{}, ErrCandidate
	}
	return owned, nil
}

// MergeParsedCandidate computes a prospective draft update. The caller MUST
// re-read authoritative draft/version inside a metadata transaction and perform
// SaveDraft(CAS), audit/outbox and candidate retention atomically. This pure
// function neither authorizes a write nor proves that a commit occurred.
func MergeParsedCandidate(current contracts.Draft, candidate ParseCandidate) (CandidateMerge, error) {
	if current.ID == "" || current.DocumentID == "" || current.RowVersion == 0 || current.ID != candidate.DraftID {
		return CandidateMerge{}, ErrCandidate
	}
	parsed := make([]contracts.ParsedBlock, len(candidate.Blocks))
	for i, block := range candidate.Blocks {
		if block.DraftID != candidate.DraftID || block.RowVersion != 0 {
			return CandidateMerge{}, ErrCandidate
		}
		parsed[i] = contracts.ParsedBlock{ID: block.ID, Order: block.Order, Content: block.Content}
	}
	ownedCandidate, err := CandidateFromParse(contracts.ParseRequest{DraftID: candidate.DraftID, Input: candidate.Input, Request: contracts.RequestContext{ProfileDigest: candidate.ProfileDigest}}, candidate.ExpectedVersion, contracts.ParseResult{Blocks: parsed, ParserVersion: candidate.ParserVersion, Diagnostics: candidate.Diagnostics})
	if err != nil {
		return CandidateMerge{}, err
	}
	var ownedDraft contracts.Draft
	if copyCandidate(current, &ownedDraft) != nil {
		return CandidateMerge{}, ErrCandidate
	}
	result := CandidateMerge{Draft: ownedDraft, Candidate: ownedCandidate}
	if current.State != "draft" {
		result.Reason = "draft_not_editable"
		return result, nil
	}
	if current.RowVersion != candidate.ExpectedVersion {
		result.Reason = "draft_changed"
		return result, nil
	}
	if len(current.Blobs) != 1 {
		result.Reason = "multiple_originals"
		return result, nil
	}
	found := false
	for _, blob := range current.Blobs {
		if blob == candidate.Input {
			found = true
		}
	}
	if !found {
		result.Reason = "original_changed"
		return result, nil
	}
	// Copy independently: edits to the returned draft must not mutate the retained
	// candidate, parser response, or current draft.
	var blocks []contracts.DraftBlock
	if copyCandidate(ownedCandidate.Blocks, &blocks) != nil {
		return CandidateMerge{}, ErrCandidate
	}
	versions := map[contracts.ID]uint64{}
	for _, block := range current.Blocks {
		if block.ID == "" || block.DraftID != current.ID || block.RowVersion == 0 || block.RowVersion == math.MaxUint64 || versions[block.ID] != 0 {
			return CandidateMerge{}, ErrCandidate
		}
		versions[block.ID] = block.RowVersion
	}
	for i := range blocks {
		blocks[i].RowVersion = versions[blocks[i].ID] + 1
	}
	result.Draft.Blocks = blocks
	result.Draft.RowVersion++
	result.Applied = true
	return result, nil
}

func candidateDigest(value contracts.Digest) bool {
	if len(value) != 64 {
		return false
	}
	raw, err := hex.DecodeString(string(value))
	return err == nil && hex.EncodeToString(raw) == string(value)
}

func copyCandidate(input, output any) error {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 8<<20 {
		return ErrCandidate
	}
	return json.Unmarshal(raw, output)
}

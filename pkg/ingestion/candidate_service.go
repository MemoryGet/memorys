package ingestion

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

var ErrCandidateRequest = errors.New("candidate processing rejected")

// DraftParseCommand is an internal application command, not an authenticated
// HTTP DTO. Host authorization must verify identity, current resource ownership,
// original-read and candidate-write rights against trusted sources.
type DraftParseCommand struct {
	Request                          contracts.RequestContext
	Principal                        contracts.Principal
	Scope                            contracts.AccessScope
	DocumentID, DraftID, CandidateID contracts.ID
	ExpectedVersion                  uint64
	Input                            contracts.BlobRef
	Format                           string
	Options                          contracts.ParseOptions
}

// PreparedCandidate keeps the exact parser result for identical commit retries.
// It is process-local; durable preparation/recovery is not implemented here.
type PreparedCandidate struct {
	command DraftParseCommand
	write   document.CandidateCommand
	valid   bool
}

type CandidateService struct {
	Parser        contracts.Parser
	Repository    document.CandidateRepository
	ProfileDigest contracts.Digest // Trusted immutable host pipeline binding.
	Authorize     func(context.Context, DraftParseCommand) error
}

func (s CandidateService) Prepare(ctx context.Context, input DraftParseCommand) (PreparedCandidate, error) {
	command, err := cloneParseCommand(input)
	if err != nil || s.Parser == nil || s.Authorize == nil || !s.valid(command) {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	c, cancel, err := candidateContext(ctx, command)
	if err != nil {
		return PreparedCandidate{}, err
	}
	defer cancel()
	if s.authorize(c, command) != nil {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	req := contracts.ParseRequest{Request: command.Request, Scope: command.Scope, DraftID: command.DraftID, Input: command.Input, Format: command.Format, Options: command.Options}
	// A parser may mutate its request slices. Keep the authoritative command and
	// candidate binding independent of that supplier's input ownership.
	parserInput, err := cloneParseCommand(command)
	if err != nil {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	req.Scope = parserInput.Scope
	req.Options = parserInput.Options
	parsed, err := s.Parser.Parse(c, req)
	if err != nil || c.Err() != nil {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	candidate, err := document.CandidateFromParse(contracts.ParseRequest{Request: command.Request, Scope: command.Scope, DraftID: command.DraftID, Input: command.Input, Format: command.Format, Options: command.Options}, command.ExpectedVersion, parsed)
	if err != nil || s.authorize(c, command) != nil {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	write := document.CandidateCommand{Request: command.Request, Scope: command.Scope, DocumentID: command.DocumentID, CandidateID: command.CandidateID, Candidate: candidate}
	return PreparedCandidate{command: command, write: write, valid: true}, nil
}

func (s CandidateService) Commit(ctx context.Context, prepared PreparedCandidate) (document.CandidateReceipt, error) {
	if !prepared.valid || s.Repository == nil || s.Authorize == nil || !s.valid(prepared.command) {
		return document.CandidateReceipt{}, ErrCandidateRequest
	}
	c, cancel, err := candidateContext(ctx, prepared.command)
	if err != nil {
		return document.CandidateReceipt{}, err
	}
	defer cancel()
	if s.authorize(c, prepared.command) != nil {
		return document.CandidateReceipt{}, ErrCandidateRequest
	}
	// Every retry receives an independently owned copy of exactly the same output.
	raw, err := json.Marshal(prepared.write)
	if err != nil || len(raw) > 12<<20 {
		return document.CandidateReceipt{}, ErrCandidateRequest
	}
	var write document.CandidateCommand
	if json.Unmarshal(raw, &write) != nil {
		return document.CandidateReceipt{}, ErrCandidateRequest
	}
	receipt, err := s.Repository.CommitCandidate(c, write)
	if err != nil {
		if errors.Is(err, document.ErrCandidateCommitUnknown) {
			return document.CandidateReceipt{}, document.ErrCandidateCommitUnknown
		}
		if errors.Is(err, document.ErrCandidateRejected) {
			return document.CandidateReceipt{}, ErrCandidateRequest
		}
		// Unclassified transport/timeouts are uncertain once dispatch occurred.
		return document.CandidateReceipt{}, document.ErrCandidateCommitUnknown
	}
	if receipt.CandidateID != prepared.write.CandidateID || receipt.DraftID != prepared.command.DraftID || receipt.DocumentID != prepared.command.DocumentID || receipt.DraftVersion == 0 {
		return document.CandidateReceipt{}, document.ErrCandidateCommitUnknown
	}
	if receipt.Applied {
		if receipt.DraftVersion != prepared.command.ExpectedVersion+1 || receipt.Reason != "" {
			return document.CandidateReceipt{}, document.ErrCandidateCommitUnknown
		}
	} else {
		switch receipt.Reason {
		case "draft_changed", "draft_not_editable", "original_changed", "multiple_originals":
		default:
			return document.CandidateReceipt{}, document.ErrCandidateCommitUnknown
		}
	}
	return receipt, nil
}

func (s CandidateService) authorize(ctx context.Context, command DraftParseCommand) error {
	owned, err := cloneParseCommand(command)
	if err != nil || s.Authorize(ctx, owned) != nil || ctx.Err() != nil {
		return ErrCandidateRequest
	}
	return nil
}

func (s CandidateService) valid(c DraftParseCommand) bool {
	if !profileDigest(s.ProfileDigest) || c.Request.ProfileDigest != s.ProfileDigest || !profileDigest(c.Input.SHA256) || c.Input.Size <= 0 || c.Input.Size > 8<<20 || c.ExpectedVersion == 0 || c.ExpectedVersion == math.MaxUint64 || c.Request.Deadline.IsZero() || c.Scope.Action != "document.parse" || c.Principal.SubjectID != c.Scope.SubjectID || c.Principal.Issuer == "" || c.Scope.PolicyVersion == 0 || len(c.Scope.KnowledgeBaseIDs) != 1 {
		return false
	}
	if c.Principal.SubjectType != "user" && c.Principal.SubjectType != "service" {
		return false
	}
	for _, id := range []contracts.ID{c.Request.RequestID, c.Scope.ScopeID, c.Scope.SubjectID, c.Scope.SpaceID, c.Scope.KnowledgeBaseIDs[0], c.DocumentID, c.DraftID, c.CandidateID, c.Input.ID} {
		if len(id) == 0 || len(id) > 200 || !utf8.ValidString(string(id)) {
			return false
		}
	}
	return c.Format != "" && len(c.Format) <= 100 && utf8.ValidString(c.Format)
}

func candidateContext(ctx context.Context, c DraftParseCommand) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrCandidateRequest
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, nil, ErrCandidateRequest
	}
	deadline := c.Request.Deadline
	if c.Scope.ExpiresAt.Before(deadline) {
		deadline = c.Scope.ExpiresAt
	}
	child, cancel := context.WithDeadline(ctx, deadline)
	if child.Err() != nil {
		cancel()
		return nil, nil, ErrCandidateRequest
	}
	return child, cancel, nil
}

func cloneParseCommand(input DraftParseCommand) (DraftParseCommand, error) {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 1<<20 {
		return DraftParseCommand{}, ErrCandidateRequest
	}
	var owned DraftParseCommand
	if json.Unmarshal(raw, &owned) != nil {
		return DraftParseCommand{}, ErrCandidateRequest
	}
	return owned, nil
}
func profileDigest(value contracts.Digest) bool {
	if len(value) != 64 {
		return false
	}
	raw, err := hex.DecodeString(string(value))
	return err == nil && hex.EncodeToString(raw) == string(value)
}

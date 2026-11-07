package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

var ErrCandidateRequest = errors.New("candidate processing rejected")
var ErrPreparedCandidateConflict = errors.New("prepared candidate already exists with different content")

type PreparedCandidateStore interface {
	Put(context.Context, contracts.ID, []byte) error
	Get(context.Context, contracts.ID) ([]byte, error)
	Delete(context.Context, contracts.ID) error
}

// FilePreparedCandidateStore is a bounded local recovery store. Files are
// private, keyed by an encoded candidate ID, and contain only the signed
// format envelope produced by CandidateService.EncodePreparedCandidate.
type FilePreparedCandidateStore struct {
	Root string
}

func NewFilePreparedCandidateStore(root string) (*FilePreparedCandidateStore, error) {
	if !filepath.IsAbs(root) || strings.TrimSpace(root) == "" {
		return nil, ErrCandidateRequest
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, ErrCandidateRequest
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrCandidateRequest
	}
	return &FilePreparedCandidateStore{Root: root}, nil
}

func (s *FilePreparedCandidateStore) path(id contracts.ID) (string, error) {
	if s == nil || !filepath.IsAbs(s.Root) || id == "" || !utf8.ValidString(string(id)) || len(id) > 200 {
		return "", ErrCandidateRequest
	}
	encoded := hex.EncodeToString([]byte(id))
	return filepath.Join(s.Root, "candidate-"+encoded+".json"), nil
}

func (s *FilePreparedCandidateStore) Put(ctx context.Context, id contracts.ID, raw []byte) error {
	if ctx == nil || ctx.Err() != nil || len(raw) == 0 || len(raw) > 16<<20 {
		return ErrCandidateRequest
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if string(existing) == string(raw) {
			return nil
		}
		return ErrPreparedCandidateConflict
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return ErrCandidateRequest
	}
	tmp, err := os.CreateTemp(s.Root, ".candidate-*.tmp")
	if err != nil {
		return ErrCandidateRequest
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil || ctx.Err() != nil {
		return ErrCandidateRequest
	}
	if err = os.Rename(tmpName, path); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && string(existing) == string(raw) {
			return nil
		}
		return ErrCandidateRequest
	}
	return nil
}

func (s *FilePreparedCandidateStore) Get(ctx context.Context, id contracts.ID) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrCandidateRequest
	}
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, ErrCandidateRequest
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil || len(raw) > 16<<20 || ctx.Err() != nil {
		return nil, ErrCandidateRequest
	}
	return raw, nil
}

func (s *FilePreparedCandidateStore) Delete(ctx context.Context, id contracts.ID) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrCandidateRequest
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrCandidateRequest
	}
	return nil
}

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
// EncodePreparedCandidate/DecodePreparedCandidate allow the opaque value to
// cross a controlled persistence boundary without turning it into authority.
type PreparedCandidate struct {
	command DraftParseCommand
	write   document.CandidateCommand
	valid   bool
}

const preparedCandidateFormat = "memorys.prepared-candidate/1"

// EncodePreparedCandidate serializes the parser output needed for a later
// commit/retry. It contains no credential secret and preserves the original
// request/scope deadlines; callers may store the returned bytes durably.
func (s CandidateService) EncodePreparedCandidate(prepared PreparedCandidate) ([]byte, error) {
	if !prepared.valid || s.valid(prepared.command) == false || prepared.write.CandidateID != prepared.command.CandidateID || prepared.write.DocumentID != prepared.command.DocumentID {
		return nil, ErrCandidateRequest
	}
	owned, err := clonePrepared(prepared)
	if err != nil {
		return nil, ErrCandidateRequest
	}
	body, err := json.Marshal(struct {
		Format  string
		Command DraftParseCommand
		Write   document.CandidateCommand
	}{preparedCandidateFormat, owned.command, owned.write})
	if err != nil || len(body) > 12<<20 {
		return nil, ErrCandidateRequest
	}
	sum := sha256.Sum256(append([]byte(preparedCandidateFormat+"\n"), body...))
	envelope := struct {
		Format  string          `json:"format"`
		SHA256  string          `json:"sha256"`
		Payload json.RawMessage `json:"payload"`
	}{preparedCandidateFormat, hex.EncodeToString(sum[:]), body}
	raw, err := json.Marshal(envelope)
	if err != nil || len(raw) > 16<<20 {
		return nil, ErrCandidateRequest
	}
	return raw, nil
}

// DecodePreparedCandidate restores an exact candidate result. Commit still
// performs current authorization and repository CAS checks, so a valid
// encoding is not a permission or publication decision.
func (s CandidateService) DecodePreparedCandidate(raw []byte) (PreparedCandidate, error) {
	if len(raw) == 0 || len(raw) > 16<<20 {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	var envelope struct {
		Format  string          `json:"format"`
		SHA256  string          `json:"sha256"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Format != preparedCandidateFormat || len(envelope.Payload) == 0 || len(envelope.SHA256) != 64 {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	sum := sha256.Sum256(append([]byte(preparedCandidateFormat+"\n"), envelope.Payload...))
	if hex.EncodeToString(sum[:]) != envelope.SHA256 {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	var stored struct {
		Format  string
		Command DraftParseCommand
		Write   document.CandidateCommand
	}
	if json.Unmarshal(envelope.Payload, &stored) != nil || stored.Format != preparedCandidateFormat || stored.Write.CandidateID != stored.Command.CandidateID || stored.Write.DocumentID != stored.Command.DocumentID {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	prepared := PreparedCandidate{command: stored.Command, write: stored.Write, valid: true}
	owned, err := clonePrepared(prepared)
	if err != nil || !s.valid(owned.command) {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	return owned, nil
}

func (s CandidateService) SavePreparedCandidate(ctx context.Context, store PreparedCandidateStore, prepared PreparedCandidate) error {
	if store == nil || !prepared.valid || prepared.command.CandidateID == "" {
		return ErrCandidateRequest
	}
	raw, err := s.EncodePreparedCandidate(prepared)
	if err != nil {
		return err
	}
	return store.Put(ctx, prepared.command.CandidateID, raw)
}

func (s CandidateService) LoadPreparedCandidate(ctx context.Context, store PreparedCandidateStore, candidateID contracts.ID) (PreparedCandidate, error) {
	if store == nil || candidateID == "" {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	raw, err := store.Get(ctx, candidateID)
	if err != nil {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	prepared, err := s.DecodePreparedCandidate(raw)
	if err != nil || prepared.command.CandidateID != candidateID {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	return prepared, nil
}

func clonePrepared(input PreparedCandidate) (PreparedCandidate, error) {
	raw, err := json.Marshal(struct {
		Command DraftParseCommand
		Write   document.CandidateCommand
	}{input.command, input.write})
	if err != nil || len(raw) > 12<<20 {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	var owned struct {
		Command DraftParseCommand
		Write   document.CandidateCommand
	}
	if json.Unmarshal(raw, &owned) != nil {
		return PreparedCandidate{}, ErrCandidateRequest
	}
	return PreparedCandidate{command: owned.Command, write: owned.Write, valid: input.valid}, nil
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

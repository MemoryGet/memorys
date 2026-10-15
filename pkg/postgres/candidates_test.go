package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// The driver double verifies authorization timing, ownership and outcome handling.
// It does NOT verify PostgreSQL syntax, locks, constraints or durable effects.
type candidateDriverState struct {
	begins, commits, rollbacks, queries int
	draft                               []byte
	update                              []driver.NamedValue
	commitErr                           error
	candidateRead                       []driver.Value
	readArgs                            []driver.NamedValue
	reviewRow                           []driver.Value
	reviewUpdate                        []driver.NamedValue
	publicationDoc                      []driver.Value
	publicationReview                   []driver.Value
	publicationPolicy                   []driver.Value
	publicationBackend                  []driver.Value
	publicationDecision                 []byte
	publicationInsert                   []driver.NamedValue
}
type candidateConnector struct{ state *candidateDriverState }

func (c candidateConnector) Connect(context.Context) (driver.Conn, error) {
	return &candidateConn{c.state}, nil
}
func (candidateConnector) Driver() driver.Driver { return candidateDriver{} }

type candidateDriver struct{}

func (candidateDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }

type candidateConn struct{ state *candidateDriverState }

func (c *candidateConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no prepare") }
func (c *candidateConn) Close() error                        { return nil }
func (c *candidateConn) Begin() (driver.Tx, error) {
	c.state.begins++
	return candidateTx{c.state}, nil
}
func (c *candidateConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *candidateConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.queries++
	if strings.HasPrefix(q, "SELECT publication_epoch::text,row_version::text") {
		return &candidateRows{columns: []string{"epoch", "version", "revision", "set", "deleted"}, values: [][]driver.Value{c.state.publicationDoc}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.publication_intents") {
		return &candidateRows{columns: []string{"digest", "receipt"}}, nil
	}
	if strings.HasPrefix(q, "SELECT q.payload,q.current_decision_id") {
		return &candidateRows{columns: []string{"request", "head", "format", "blocks", "content"}, values: [][]driver.Value{c.state.publicationReview}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.review_policy_heads") {
		return &candidateRows{columns: []string{"version", "head", "digest", "payload"}, values: [][]driver.Value{c.state.publicationPolicy}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.publication_backend_heads") {
		return &candidateRows{columns: []string{"head", "id", "payload"}, values: [][]driver.Value{c.state.publicationBackend}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.review_decisions") {
		return &candidateRows{columns: []string{"payload"}, values: [][]driver.Value{{c.state.publicationDecision}}}, nil
	}
	if strings.HasPrefix(q, "SELECT c.payload,c.receipt,c.actor_id,c.request_digest") {
		c.state.readArgs = append([]driver.NamedValue(nil), args...)
		rows := &candidateRows{columns: []string{"payload", "receipt", "actor", "digest"}}
		if c.state.candidateRead != nil {
			rows.values = [][]driver.Value{c.state.candidateRead}
		}
		return rows, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.documents") {
		return &candidateRows{columns: []string{"alive"}, values: [][]driver.Value{{true}}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.draft_candidates") {
		return &candidateRows{columns: []string{"digest", "receipt"}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.submission_receipts") {
		return &candidateRows{columns: []string{"digest", "receipt"}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.review_receipts") {
		return &candidateRows{columns: []string{"digest", "receipt"}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.approval_requests q") {
		return &candidateRows{columns: []string{"payload", "version", "format", "blocks", "content", "current"}, values: [][]driver.Value{c.state.reviewRow}}, nil
	}
	if strings.Contains(q, "FROM akos_withdraw_v1.draft_heads") {
		return &candidateRows{columns: []string{"payload", "version"}, values: [][]driver.Value{{c.state.draft, "1"}}}, nil
	}
	return nil, errors.New("unexpected query")
}
func (c *candidateConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO akos_withdraw_v1.publication_intents") {
		c.state.publicationInsert = append([]driver.NamedValue(nil), args...)
	}
	if strings.HasPrefix(q, "UPDATE akos_withdraw_v1.draft_heads") {
		c.state.update = append([]driver.NamedValue(nil), args...)
	}
	if strings.HasPrefix(q, "UPDATE akos_withdraw_v1.approval_requests") {
		c.state.reviewUpdate = append([]driver.NamedValue(nil), args...)
	}
	return driver.RowsAffected(1), nil
}

type candidateTx struct{ state *candidateDriverState }

func (t candidateTx) Commit() error   { t.state.commits++; return t.state.commitErr }
func (t candidateTx) Rollback() error { t.state.rollbacks++; return nil }

type candidateRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *candidateRows) Columns() []string { return r.columns }
func (r *candidateRows) Close() error      { return nil }
func (r *candidateRows) Next(values []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(values, r.values[0])
	r.values = r.values[1:]
	return nil
}

func candidateCommandFixture(t *testing.T) (context.Context, context.CancelFunc, document.CandidateCommand, *sql.DB, *candidateDriverState) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	deadline, _ := ctx.Deadline()
	blob := contracts.BlobRef{ID: "blob", Size: 100, SHA256: contracts.Digest(strings.Repeat("a", 64))}
	request := contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), Deadline: deadline}
	candidate, err := document.CandidateFromParse(contracts.ParseRequest{Request: request, DraftID: "draft", Input: blob}, 1, contracts.ParseResult{ParserVersion: "fixture/1", Blocks: []contracts.ParsedBlock{{ID: "block", Content: contracts.Content{Kind: contracts.TextBlock, Text: "candidate", Source: contracts.SourceAnchor{Kind: "original", BlobID: blob.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	command := document.CandidateCommand{Request: request, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "actor", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "document.parse", PolicyVersion: 1, ExpiresAt: deadline}, DocumentID: "document", CandidateID: "candidate", Candidate: candidate}
	raw, _ := json.Marshal(contracts.Draft{ID: "draft", DocumentID: "document", State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{blob}})
	state := &candidateDriverState{draft: raw}
	db := sql.OpenDB(candidateConnector{state})
	t.Cleanup(func() { db.Close(); cancel() })
	return ctx, cancel, command, db, state
}

func TestCandidateRequiresResourceAuthorizationBeforeSQL(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "missing", false: "denied"}[missing], func(t *testing.T) {
			ctx, _, command, db, state := candidateCommandFixture(t)
			repository := Candidates{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}
			if !missing {
				repository.AuthorizeCandidate = func(context.Context, document.CandidateCommand) error { return errors.New("resource denied") }
			}
			receipt, err := repository.CommitCandidate(ctx, command)
			if err == nil || receipt.CandidateID != "" || state.begins != 0 || state.queries != 0 {
				t.Fatalf("scope authorized resource implicitly: %+v %v %+v", receipt, err, state)
			}
		})
	}
}

func TestCandidateResourceRevocationRollsBackBeforeCommit(t *testing.T) {
	ctx, _, command, db, state := candidateCommandFixture(t)
	calls := 0
	repository := Candidates{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeCandidate: func(context.Context, document.CandidateCommand) error {
		calls++
		if calls == 3 {
			return errors.New("revoked")
		}
		return nil
	}}
	receipt, err := repository.CommitCandidate(ctx, command)
	if err == nil || receipt.CandidateID != "" || calls != 3 || state.commits != 0 || state.rollbacks != 1 || len(state.update) == 0 {
		t.Fatalf("resource revocation did not abort: %+v %v %+v", receipt, err, state)
	}
}

func TestCandidateAuthorizerCannotRewriteSQLTargetOrContent(t *testing.T) {
	ctx, _, command, db, state := candidateCommandFixture(t)
	calls := 0
	repository := Candidates{DB: db, Verify: func(_ context.Context, s contracts.AccessScope) error { s.KnowledgeBaseIDs[0] = "mutated"; return nil }, AuthorizeCandidate: func(_ context.Context, c document.CandidateCommand) error {
		calls++
		if c.DocumentID != "document" || c.Scope.KnowledgeBaseIDs[0] != "kb" || c.Candidate.Blocks[0].Content.Text != "candidate" {
			return errors.New("mutated command reused")
		}
		c.DocumentID = "other"
		c.Scope.KnowledgeBaseIDs[0] = "other"
		c.Candidate.Blocks[0].Content.Text = "rewritten"
		return nil
	}}
	receipt, err := repository.CommitCandidate(ctx, command)
	if err != nil || !receipt.Applied || calls != 3 || state.commits != 1 || len(state.update) != 7 {
		t.Fatalf("commit: %+v %v %+v", receipt, err, state)
	}
	if state.update[1].Value != "kb" || state.update[2].Value != "document" || strings.Contains(state.update[6].Value.(string), "rewritten") || command.Candidate.Blocks[0].Content.Text != "candidate" || command.Scope.KnowledgeBaseIDs[0] != "kb" {
		t.Fatal("authorizer mutated write or caller data")
	}
}

func TestCandidateFreezesCallerBackingArraysBeforeAuthorization(t *testing.T) {
	ctx, _, command, db, state := candidateCommandFixture(t)
	repository := Candidates{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeCandidate: func(_ context.Context, owned document.CandidateCommand) error {
		if owned.Scope.KnowledgeBaseIDs[0] != "kb" || owned.Candidate.Blocks[0].Content.Text != "candidate" {
			return errors.New("unstable authorization target")
		}
		command.Scope.KnowledgeBaseIDs[0] = "external-change"
		command.Candidate.Blocks[0].Content.Text = "external-change"
		return nil
	}}
	receipt, err := repository.CommitCandidate(ctx, command)
	if err != nil || !receipt.Applied || state.commits != 1 || len(state.update) != 7 {
		t.Fatalf("caller alias changed frozen command: %+v %v", receipt, err)
	}
	if state.update[1].Value != "kb" || strings.Contains(state.update[6].Value.(string), "external-change") {
		t.Fatal("candidate/digest and applied content diverged")
	}
}

func TestCandidateCommitErrorPreservesPlatformUnknownOutcome(t *testing.T) {
	ctx, _, command, db, state := candidateCommandFixture(t)
	state.commitErr = errors.New("driver lost commit response")
	repository := Candidates{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeCandidate: func(context.Context, document.CandidateCommand) error { return nil }}
	receipt, err := repository.CommitCandidate(ctx, command)
	if !errors.Is(err, ErrCommitUnknown) || !errors.Is(err, document.ErrCandidateCommitUnknown) || errors.Is(err, document.ErrCandidateRejected) || receipt.CandidateID != "" || state.commits != 1 {
		t.Fatalf("uncertain commit classified as rejection: %+v %v", receipt, err)
	}
}

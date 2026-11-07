package localcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestStateIntegrityAndParsing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	s, err := createState(root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := createState(root)
	if err != nil || !reflect.DeepEqual(again.m, s.m) {
		t.Fatal("state changed on retry", err)
	}
	host, err := loadState(root, false)
	if err != nil || host.key != nil {
		t.Fatal("HTTP host loaded signing key")
	}
	for _, text := range []string{"", " \n\t", string([]byte{0xff}), "a\x00b", strings.Repeat("a", maxTextBytes+1)} {
		if _, err = s.textDraft(text); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
	one, _ := s.textDraft("markdown **body**")
	two, _ := s.textDraft("markdown **body**")
	changed, _ := s.textDraft("changed")
	if locator := one.Blocks[0].Content.Source.Locator; locator.Precision != "exact" || locator.Characters == nil || locator.Characters.Start != 0 || locator.Characters.End != len([]rune(one.Blocks[0].Content.Text)) {
		t.Fatal("text draft lost exact character locator")
	}
	legacy := one
	legacy.Blocks = append([]contracts.DraftBlock(nil), one.Blocks...)
	legacy.Blocks[0].Content.Source.Locator = contracts.Locator{Precision: "coarse"}
	if !textDraftMatches(legacy, one) {
		t.Fatal("legacy coarse text draft was not accepted")
	}
	if one.ID != two.ID || one.DocumentID == changed.DocumentID {
		t.Fatal("content identity differs")
	}
	base := contracts.ID("revision-current")
	edited, err := s.textDraftForDocument(one.DocumentID, &base, "markdown **edited**")
	if err != nil || edited.DocumentID != one.DocumentID || edited.BaseRevisionID == nil || *edited.BaseRevisionID != base || edited.ID == one.ID {
		t.Fatal("edited draft did not preserve document lineage")
	}
	if got, err := s.textDraft(one.Blocks[0].Content.Text); err != nil || !reflect.DeepEqual(got, one) {
		t.Fatal("legacy draft identity changed")
	}
	if _, err = parse([]string{"publish", string(one.ID), "--expect-hash", strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"serve", "--address", "0.0.0.0:8080"}, {"search", "hi", "--address", "example.com:80"}, {"publish", "draft"}, {"import"}, {"doctor", "--with-samples"}} {
		if _, err = parse(args); err == nil {
			t.Fatal("unsafe CLI accepted", args)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), append(encode(s.m), []byte("{}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadState(root, true); err == nil {
		t.Fatal("trailing manifest accepted")
	}
}

func TestExactPDFOriginalDraftIntegrity(t *testing.T) {
	s := &state{m: manifest{Features: 1, Installation: "installation-123456789012345678901234567890", ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}
	blob := contracts.BlobRef{ID: "blob", SHA256: contracts.Digest(strings.Repeat("b", 64)), Size: 32, MediaType: mediaFormats["pdf"]}
	parserVersion := "pypdf/6.19.0"
	id := digest([]byte(string(s.m.Installation) + "/original/pdf/" + string(blob.SHA256) + "/" + parserVersion))
	draftID := contracts.ID("draft-original-" + id)
	page := 2
	text := "第二页正文"
	blockID := contracts.ID(digest([]byte(string(draftID) + "\x00" + string(blob.SHA256) + "\x00pdf-page\x00" + strconv.Itoa(page) + "\x00" + text + "\x00" + string(s.m.ProfileDigest) + "\x00" + parserVersion)))
	draft := contracts.Draft{ID: draftID, DocumentID: contracts.ID("doc-original-" + id), State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{blob}, Blocks: []contracts.DraftBlock{{ID: blockID, DraftID: draftID, RowVersion: 1, Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: text, Source: contracts.SourceAnchor{Kind: "original", BlobID: blob.ID, Locator: contracts.Locator{Precision: "exact", Page: &page, Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCountInString(text)}}}}}}}
	if err := s.validateOriginalDraft(draft); err != nil {
		t.Fatalf("valid exact PDF draft rejected: %v", err)
	}
	draft.Blocks[0].Content.Text = "tampered"
	if err := s.validateOriginalDraft(draft); err == nil {
		t.Fatal("tampered exact PDF draft accepted")
	}
}

// Run only against the disposable cluster prepared by check_local_cli_windows.py.
// Never inherit MEMORYS_DATABASE_URL as implicit integration authorization.
func TestPostgresLifecycle(t *testing.T) {
	u := os.Getenv("MEMORYS_LOCAL_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	t.Setenv("MEMORYS_DATABASE_URL", u)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, err := openDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := filepath.Join(t.TempDir(), "runtime")
	s, err := createState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.initialize(ctx, db); err != nil {
		t.Fatal("initialize", err)
	}
	if err = s.initialize(ctx, db); err != nil {
		t.Fatal("initialize replay", err)
	}
	owner, err := s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	h, err := handler(db, s)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	search := func(query string) []json.RawMessage {
		t.Helper()
		raw, err := searchHTTP(ctx, s, owner, address, query)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Evidence []json.RawMessage `json:"evidence"`
		}
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("bad search response")
		}
		return result.Evidence
	}
	imported, err := s.importText(ctx, db, owner, "localinitialprobe\n这是本机发布的真实正文。")
	if err != nil {
		t.Fatal(err)
	}
	if got := search("localinitialprobe"); len(got) != 0 {
		t.Fatal("draft exposed")
	}
	if _, err = s.publish(ctx, db, owner, imported.DraftID, contracts.Digest(strings.Repeat("0", 64)), nil); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	first, err := s.publish(ctx, db, owner, imported.DraftID, imported.ContentHash, nil)
	if err != nil {
		t.Fatal("publish", err)
	}
	if got := search("localinitialprobe"); len(got) != 1 || !bytes.Contains(got[0], []byte("localinitialprobe")) || !bytes.Contains(got[0], []byte(`"kind":"revision"`)) {
		t.Fatal("real revision evidence absent", string(encode(got)))
	}
	edited, err := s.editDraft(ctx, db, owner, imported.DraftID, "localinitialprobe v2\n这是替换后的真实正文。")
	if err != nil {
		t.Fatal("edit draft", err)
	}
	if edited.DocumentID != imported.DocumentID || edited.DraftID == imported.DraftID {
		t.Fatal("edited draft lost document lineage")
	}
	feedbackHost := &webHost{db: db, s: s}
	feedback := []localFeedback{
		{ID: "feedback-for-edit", DocumentID: edited.DocumentID, ProposalDraftID: edited.DraftID, State: "open"},
		{ID: "feedback-for-other-document", DocumentID: "another-document", ProposalDraftID: edited.DraftID, State: "resolved"},
		{ID: "feedback-without-proposal", DocumentID: edited.DocumentID, State: "open"},
	}
	rows, err := feedbackHost.feedbackPublications(ctx, feedback)
	if err != nil || len(rows) != 3 || rows[0].Publication != nil {
		t.Fatal("unpublished proposal has a publication", rows, err)
	}
	editedPublication, err := s.publish(ctx, db, owner, edited.DraftID, edited.ContentHash, nil)
	if err != nil {
		t.Fatal("publish edited draft", err)
	}
	rows, err = feedbackHost.feedbackPublications(ctx, feedback)
	if err != nil || len(rows) != 3 {
		t.Fatal("read feedback publication", rows, err)
	}
	link := rows[0].Publication
	history := editedPublication.History
	if link == nil || history.RevisionID == nil || link.RevisionID != *history.RevisionID || link.HistoryID != history.ID || !link.PublishedAt.Equal(history.OccurredAt) {
		t.Fatal("wrong feedback publication", link, history)
	}
	if rows[0].State != "open" || rows[1].Publication != nil || rows[2].Publication != nil {
		t.Fatal("feedback state or document binding changed", rows)
	}
	if got := search("localinitialprobe v2"); len(got) != 1 || !bytes.Contains(got[0], []byte("替换后的真实正文")) {
		t.Fatal("edited revision not searchable", string(encode(got)))
	}
	replayed, err := s.publish(ctx, db, owner, edited.DraftID, edited.ContentHash, nil)
	if err != nil || replayed.History.ID == first.History.ID {
		t.Fatal("publish replay", err)
	}
	withdrawn, err := s.withdraw(ctx, db, owner, imported.DocumentID, "lifecycle integration withdrawal")
	if err != nil || withdrawn.DocumentID != imported.DocumentID {
		t.Fatal("withdraw", err)
	}
	if got := search("localinitialprobe v2"); len(got) != 0 {
		t.Fatal("withdrawn document remained searchable", string(encode(got)))
	}
	if _, err = s.publishDraft(ctx, db, owner, edited.DraftID, edited.ContentHash, nil, true); err == nil || !strings.Contains(err.Error(), "先确认重新发布") {
		t.Fatal("retry started a new publication after withdrawal", err)
	}
	if _, err = s.publish(ctx, db, owner, imported.DraftID, imported.ContentHash, nil); err == nil || !strings.Contains(err.Error(), "发布版本已变化") {
		t.Fatal("obsolete draft accepted after withdrawal", err)
	}
	interrupted := errors.New("interrupt explicit republication")
	if _, err = s.publish(ctx, db, owner, edited.DraftID, edited.ContentHash, func(stage string) error {
		if stage == "started" {
			return interrupted
		}
		return nil
	}); !errors.Is(err, interrupted) {
		t.Fatal("start republication", err)
	}
	republished, err := s.publishDraft(ctx, db, owner, edited.DraftID, edited.ContentHash, nil, true)
	if err != nil || republished.Operation.ID == editedPublication.Operation.ID ||
		republished.Operation.RevisionID != editedPublication.Operation.RevisionID ||
		republished.History.PublicationEpoch != withdrawn.PublicationEpoch+1 ||
		republished.History.GenerationSetID == nil || editedPublication.History.GenerationSetID == nil ||
		*republished.History.GenerationSetID == *editedPublication.History.GenerationSetID {
		t.Fatal("republish after withdrawal did not produce a new publication", republished, err)
	}
	replayed, err = s.publish(ctx, db, owner, edited.DraftID, edited.ContentHash, nil)
	if err != nil || replayed.History.ID != republished.History.ID {
		t.Fatal("republish replay", err)
	}
	job, err := publicationJob(ctx, db, s.m.Space, s.m.KB, edited.DraftID)
	if err != nil || job == nil || job.OperationID != republished.Operation.ID {
		t.Fatal("preview did not select the latest publication job", job, err)
	}
	if got := search("localinitialprobe v2"); len(got) != 1 {
		t.Fatal("republished document is not searchable", string(encode(got)))
	}
	duplicate, err := s.importText(ctx, db, owner, "localinitialprobe\n这是本机发布的真实正文。")
	if err != nil || !duplicate.Reused || duplicate.DraftID != imported.DraftID {
		t.Fatal("submitted draft import replay", err)
	}
	deleted, err := s.deleteDocument(ctx, db, owner, imported.DocumentID)
	if err != nil || deleted.DocumentID != imported.DocumentID || deleted.JobID == "" {
		t.Fatal("delete", err)
	}
	if got := search("localinitialprobe v2"); len(got) != 0 {
		t.Fatal("deleted document remained searchable", string(encode(got)))
	}
	var cleanupState string
	if err = db.QueryRowContext(ctx, `SELECT p.state FROM akos_withdraw_v1.document_delete_job_progress p WHERE p.job_id=$1`, deleted.JobID).Scan(&cleanupState); err != nil || cleanupState != "queued" {
		t.Fatalf("delete cleanup job state = %q, err=%v", cleanupState, err)
	}
	if _, err := s.importText(ctx, db, owner, "localinitialprobe\n这是本机发布的真实正文。"); !errors.Is(err, errImport) {
		t.Fatal("reimport after logical deletion should be rejected", err)
	}
	for _, stage := range []string{"submitted", "approved", "started", "claimed", "prepared", "completed"} {
		t.Run("recover_"+stage, func(t *testing.T) {
			r, err := s.importText(ctx, db, owner, "localrecover"+stage+"\n真实故障恢复正文")
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected process stop")
			_, err = s.publish(ctx, db, owner, r.DraftID, r.ContentHash, func(at string) error {
				if at == stage {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatal("stop point not reached", err)
			}
			fresh, err := s.identity(ctx, db, false)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.close()
			if _, err = s.publish(ctx, db, fresh, r.DraftID, r.ContentHash, nil); err != nil {
				t.Fatal("resume", err)
			}
			if len(search("localrecover"+stage)) != 1 {
				t.Fatal("recovered document not searchable")
			}
		})
	}
	t.Run("expired_lease", func(t *testing.T) {
		r, err := s.importText(ctx, db, owner, "localexpiredlease")
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.publish(ctx, db, owner, r.DraftID, r.ContentHash, func(at string) error {
			if at == "claimed" {
				return io.EOF
			}
			return nil
		})
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		// PostgreSQL grants the lease using its clock, which can diverge from
		// the test host's clock when the fixture runs in WSL.
		deadline := time.Now().Add(time.Minute)
		for {
			var expired bool
			err := db.QueryRowContext(ctx, `SELECT h.lease_until::timestamptz <= clock_timestamp()
FROM akos_withdraw_v1.publication_task_heads h
JOIN akos_withdraw_v1.publication_intents i ON i.space_id=h.space_id AND i.kb_id=h.kb_id AND i.job_id=h.id
WHERE i.space_id=$1 AND i.kb_id=$2 AND i.document_id=$3`, s.m.Space, s.m.KB, r.DocumentID).Scan(&expired)
			if err != nil {
				t.Fatal("read fixture lease expiration", err)
			}
			if expired {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fixture lease did not expire in PostgreSQL")
			}
			time.Sleep(250 * time.Millisecond)
		}
		if _, err = s.publish(ctx, db, owner, r.DraftID, r.ContentHash, nil); err != nil {
			t.Fatal("expired lease recovery", err)
		}
		var attempt int
		if err := db.QueryRowContext(ctx, `SELECT h.attempt::text FROM akos_withdraw_v1.publication_task_heads h JOIN akos_withdraw_v1.publication_intents i ON i.job_id=h.id WHERE i.document_id=$1`, r.DocumentID).Scan(&attempt); err != nil || attempt != 2 {
			t.Fatalf("expected fenced attempt 2, got %d: %v", attempt, err)
		}
	})
	for _, credential := range []string{"invalid", string(owner.credential.Secret)} {
		if credential != "invalid" {
			if _, err = db.ExecContext(ctx, "UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer=$1 AND subject_id=$2", s.m.Issuer, s.m.Owner); err != nil {
				t.Fatal(err)
			}
		}
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/knowledge-os/search", bytes.NewReader(encode(map[string]any{"query": "localinitialprobe", "space_id": s.m.Space, "kb_ids": []contracts.ID{s.m.KB}})))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 401 || bytes.Contains(body, []byte("localinitialprobe")) {
			t.Fatal("revoked/invalid token disclosed evidence", resp.StatusCode)
		}
	}
	owner.close()
	owner, err = s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	var out bytes.Buffer
	if err = Run(ctx, []string{"init", "--state", root, "--with-samples"}, &out); err != nil {
		t.Fatal("sample init", err)
	}
	out.Reset()
	if err = Run(ctx, []string{"init", "--state", root, "--with-samples"}, &out); err != nil {
		t.Fatal("sample retry", err)
	}
	if len(search("memoryslocalsample")) != 1 {
		t.Fatal("sample duplication")
	}
	// A new sealed ACL takes effect without restarting the HTTP host. Init must
	// reject it, rather than re-grant the revoked permission or rewind its head.
	if _, err = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,4,false)`, s.m.Space); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants SELECT space_id,4,issuer,subject_id,client_id,kb_id,action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=3 AND action<>'knowledge.search'`, s.m.Space); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=4`, s.m.Space); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=4 WHERE space_id=$1`, s.m.Space); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.scope(ctx, "knowledge.search", ""); err == nil {
		t.Fatal("revoked search grant accepted")
	}
	if _, err = searchHTTP(ctx, s, owner, address, "localinitialprobe"); err == nil {
		t.Fatal("revoked ACL allowed HTTP search")
	}
	if err = s.initialize(ctx, db); err == nil {
		t.Fatal("init overwrote current ACL")
	}
	var version int
	if db.QueryRowContext(ctx, "SELECT version FROM akos_withdraw_v1.access_policy_heads WHERE space_id=$1", s.m.Space).Scan(&version) != nil || version != 4 {
		t.Fatal("ACL changed by init")
	}
	out.Reset()
	if err = doctor(ctx, root, &out); err == nil || !bytes.Contains(out.Bytes(), []byte("nothing was changed")) {
		t.Fatal("doctor accepted configuration conflict")
	}
}

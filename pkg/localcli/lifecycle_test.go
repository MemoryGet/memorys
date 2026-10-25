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
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestStateIntegrityAndParsing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	s, err := createState(root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := createState(root)
	if err != nil || again.m != s.m {
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
	if one.ID != two.ID || one.DocumentID == changed.DocumentID {
		t.Fatal("content identity differs")
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
	replayed, err := s.publish(ctx, db, owner, imported.DraftID, imported.ContentHash, nil)
	if err != nil || replayed.History.ID != first.History.ID {
		t.Fatal("publish replay", err)
	}
	duplicate, err := s.importText(ctx, db, owner, "localinitialprobe\n这是本机发布的真实正文。")
	if err != nil || !duplicate.Reused || duplicate.DraftID != imported.DraftID {
		t.Fatal("submitted draft import replay", err)
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
		time.Sleep(31 * time.Second)
		if _, err = s.publish(ctx, db, owner, r.DraftID, r.ContentHash, nil); err != nil {
			t.Fatal("expired lease recovery", err)
		}
		var attempt int
		if db.QueryRowContext(ctx, `SELECT h.attempt FROM akos_withdraw_v1.publication_task_heads h JOIN akos_withdraw_v1.publication_intents i ON i.job_id=h.id WHERE i.document_id=$1`, r.DocumentID).Scan(&attempt) != nil || attempt != 2 {
			t.Fatal("new fenced attempt absent")
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
	if _, err = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,2,true)`, s.m.Space); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=2 WHERE space_id=$1`, s.m.Space); err != nil {
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
	if db.QueryRowContext(ctx, "SELECT version FROM akos_withdraw_v1.access_policy_heads WHERE space_id=$1", s.m.Space).Scan(&version) != nil || version != 2 {
		t.Fatal("ACL changed by init")
	}
	out.Reset()
	if err = doctor(ctx, root, &out); err == nil || !bytes.Contains(out.Bytes(), []byte("nothing was changed")) {
		t.Fatal("doctor accepted configuration conflict")
	}
}

package localcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

// Only an explicitly supplied disposable DB may run these fault injections.
// OpenCorpus fills the request-local digest memo before storage is changed.
func TestPostgresArtifactDigestMemoFreshness(t *testing.T) {
	u := os.Getenv("MEMORYS_LOCAL_AI_TEST_DATABASE_URL")
	if u == "" || os.Getenv("MEMORYS_LOCAL_MEMO_FAULT_INJECTION") != "1" {
		t.Skip("explicitly enabled isolated AI fixture required")
	}
	t.Setenv("MEMORYS_DATABASE_URL", u)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, err := openDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := createState(filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal("create state", err)
	}
	if err = s.initialize(ctx, db); err != nil {
		t.Fatal("initialize", err)
	}
	spec := contracts.EmbeddingSpec{ProviderModel: "ollama/guard-fixture:latest", ModelRevision: strings.Repeat("a", 64), Dimension: 16, Distance: "cosine", Normalization: "l2", PreprocessVersion: "utf8-exact/1"}
	if err := s.upgradeAI(ctx, db, spec); err != nil {
		t.Fatal("enable AI", err)
	}
	a, err := s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	doc, err := s.importText(ctx, db, a, "digest memo guard: 原始证据必须保持正确。")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.publish(ctx, db, a, doc.DraftID, doc.ContentHash, nil); err != nil {
		t.Fatal(err)
	}
	service, err := s.searchService(db)
	if err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline()
	corpus, err := service.OpenCorpus(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline,
		SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: "证据", Limit: 10})
	if err != nil {
		t.Fatal("open corpus", err)
	}
	defer corpus.Close()
	if len(corpus.Candidates) != 1 || corpus.Validate(ctx, corpus.Candidates) != nil {
		t.Fatal("baseline validation")
	}
	candidate := corpus.Candidates[0]
	if len(candidate.GenerationIDs) != 1 {
		t.Fatal("unexpected generations")
	}
	gen, docID := candidate.GenerationIDs[0], candidate.Citation.DocumentID
	var artifact, index, frozen []byte
	if db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_prepared_artifacts WHERE generation_id=$1`, gen).Scan(&artifact) != nil ||
		db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1`, gen).Scan(&index) != nil ||
		db.QueryRowContext(ctx, `SELECT frozen_payload FROM akos_withdraw_v1.revision_contents WHERE document_id=$1`, docID).Scan(&frozen) != nil {
		t.Fatal("read owned fixture")
	}
	fault := func(query string, args ...any) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, "SET LOCAL session_replication_role='replica'"); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if count, err := result.RowsAffected(); err != nil || count != 1 {
			return fmt.Errorf("expected one mutated fixture row: %d: %v", count, err)
		}
		return tx.Commit()
	}
	tests := []struct {
		name            string
		mutate, restore func() error
	}{
		{"changed_embedded_digest_on_memo_hit", func() error {
			return fault(`WITH a AS (UPDATE akos_withdraw_v1.publication_prepared_artifacts SET payload=jsonb_set(payload,'{Digest}',to_jsonb(repeat('0',64)),false),artifact_digest=repeat('0',64) WHERE generation_id=$1 RETURNING operation_id) UPDATE akos_withdraw_v1.publication_finalizations f SET artifact_digest=repeat('0',64) FROM a WHERE f.operation_id=a.operation_id`, gen)
		}, func() error {
			return fault(`WITH a AS (UPDATE akos_withdraw_v1.publication_prepared_artifacts SET payload=$2::jsonb,artifact_digest=($2::jsonb)->>'Digest' WHERE generation_id=$1 RETURNING operation_id) UPDATE akos_withdraw_v1.publication_finalizations f SET artifact_digest=($2::jsonb)->>'Digest' FROM a WHERE f.operation_id=a.operation_id`, gen, string(artifact))
		}},
		{"changed_artifact_body", func() error {
			return fault(`UPDATE akos_withdraw_v1.publication_prepared_artifacts SET payload=jsonb_set(payload,'{GenerationSet,State}',to_jsonb('tampered'::text),false) WHERE generation_id=$1`, gen)
		}, func() error {
			return fault(`UPDATE akos_withdraw_v1.publication_prepared_artifacts SET payload=$2::jsonb WHERE generation_id=$1`, gen, string(artifact))
		}},
		{"changed_index", func() error {
			return fault(`UPDATE akos_withdraw_v1.index_projection_rows SET payload='{}'::jsonb WHERE generation_id=$1`, gen)
		}, func() error {
			return fault(`UPDATE akos_withdraw_v1.index_projection_rows SET payload=$2::jsonb WHERE generation_id=$1`, gen, string(index))
		}},
		{"changed_frozen_body", func() error {
			return fault(`UPDATE akos_withdraw_v1.revision_contents SET frozen_payload=$2 WHERE document_id=$1`, docID, []byte("{}"))
		}, func() error {
			return fault(`UPDATE akos_withdraw_v1.revision_contents SET frozen_payload=$2 WHERE document_id=$1`, docID, frozen)
		}},
		{"revoked_token", func() error {
			_, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer=$1 AND subject_id=$2`, s.m.Issuer, s.m.Owner)
			return err
		}, func() error {
			_, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer=$1 AND subject_id=$2`, s.m.Issuer, s.m.Owner)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.mutate(); err != nil {
				t.Fatal("inject fault", err)
			}
			defer func() {
				if err := test.restore(); err != nil {
					t.Fatal("restore fault", err)
				}
			}()
			if test.name == "changed_embedded_digest_on_memo_hit" || test.name == "changed_artifact_body" {
				var joined int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.publication_prepared_artifacts a JOIN akos_withdraw_v1.publication_finalizations f ON f.operation_id=a.operation_id AND f.artifact_digest=a.artifact_digest WHERE a.generation_id=$1`, gen).Scan(&joined); err != nil || joined != 1 {
					t.Fatal("fault lost the native finalization join", joined, err)
				}
			}
			if err := corpus.Validate(ctx, corpus.Candidates); err == nil {
				t.Fatal("changed source or authority accepted")
			}
		})
		if corpus.Validate(ctx, corpus.Candidates) != nil {
			t.Fatal("restored fixture no longer validates after", test.name)
		}
	}
}

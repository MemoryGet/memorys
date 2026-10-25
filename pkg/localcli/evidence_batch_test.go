package localcli

import (
	"context"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func TestPostgresEvidenceBatchFreshness(t *testing.T) {
	db, root := performanceDB(t, &performanceTrace{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := loadState(root, true)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	service, err := s.searchService(db)
	if err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline()
	request := contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: s.m.ProfileDigest, Deadline: deadline}
	corpus, err := service.OpenCorpus(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: request.RequestID,
		Deadline: deadline, SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: "perfdoc0000", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Candidates) == 0 {
		t.Fatal("owned published fixture required")
	}
	candidate := corpus.Candidates[0]
	corpus.Close()
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil {
		t.Fatal(err)
	}
	reads := postgres.QueryReads{DB: db, Verify: a.verify}
	observed, err := queryread.ObserveCurrent(ctx, reads, scope, []contracts.ID{candidate.Citation.DocumentID}, queryread.CurrentLimits{MaxDocuments: 1, MaxBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	evidence := []contracts.Evidence{{Citation: candidate.Citation, SourceExcerpt: candidate.Text, Rank: 1}}
	if (postgres.EvidenceBackend{Reads: reads}).Verify(ctx, request, scope, observed, evidence) != nil {
		t.Fatal("baseline evidence rejected")
	}
	var frozen, index []byte
	doc := candidate.Citation.DocumentID
	gen := candidate.GenerationIDs[0]
	if db.QueryRowContext(ctx, "SELECT frozen_payload FROM akos_withdraw_v1.revision_contents WHERE document_id=$1", doc).Scan(&frozen) != nil {
		t.Fatal("frozen fixture")
	}
	if db.QueryRowContext(ctx, "SELECT payload FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1", gen).Scan(&index) != nil {
		t.Fatal("index fixture")
	}
	// Only this explicitly supplied disposable fixture is allowed to inject
	// storage corruption. Bypass immutable-table triggers transaction-locally;
	// normal sessions and application SQL guards are never changed.
	fault := func(query string, args ...any) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, "SET LOCAL session_replication_role='replica'"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
		return tx.Commit()
	}
	cases := []struct {
		name            string
		mutate, restore func() error
	}{
		{"frozen_corruption_between_views", func() error {
			return fault("UPDATE akos_withdraw_v1.revision_contents SET frozen_payload=$2 WHERE document_id=$1", doc, []byte("{}"))
		},
			func() error {
				return fault("UPDATE akos_withdraw_v1.revision_contents SET frozen_payload=$2 WHERE document_id=$1", doc, frozen)
			}},
		{"index_corruption_between_views", func() error {
			return fault("UPDATE akos_withdraw_v1.index_projection_rows SET payload='{}'::jsonb WHERE generation_id=$1", gen)
		},
			func() error {
				return fault("UPDATE akos_withdraw_v1.index_projection_rows SET payload=$2::jsonb WHERE generation_id=$1", gen, string(index))
			}},
		{"publication_epoch_change_between_views", func() error {
			return fault("UPDATE akos_withdraw_v1.documents SET publication_epoch=publication_epoch+1 WHERE id=$1", doc)
		},
			func() error {
				return fault("UPDATE akos_withdraw_v1.documents SET publication_epoch=publication_epoch-1 WHERE id=$1", doc)
			}},
		{"revocation_between_views", func() error {
			_, e := db.ExecContext(ctx, "UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer=$1 AND subject_id=$2", s.m.Issuer, s.m.Owner)
			return e
		},
			func() error {
				_, e := db.ExecContext(ctx, "UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer=$1 AND subject_id=$2", s.m.Issuer, s.m.Owner)
				return e
			}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			mutated := false
			defer func() {
				if mutated {
					if err := test.restore(); err != nil {
						t.Fatal("fixture restoration", err)
					}
				}
			}()
			backend := postgres.EvidenceBackend{Reads: postgres.QueryReads{DB: db, Verify: func(c context.Context, q contracts.AccessScope) error {
				calls++
				if calls == 3 {
					if err := test.mutate(); err != nil {
						return err
					}
					mutated = true
				}
				return a.verify(c, q)
			}}}
			if err := backend.Verify(ctx, request, scope, observed, evidence); err == nil || !mutated {
				t.Fatal("changed authority/source accepted", calls, err)
			}
		})
	}
}

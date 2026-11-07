package localcli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/migration"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const schemaChecksum = "1d0556f2598df5f302135f8bada7f9a934e898a6594eefebce0acdf2f145b39a"
const localACLCheck = `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.access_policy_heads h JOIN akos_withdraw_v1.access_policy_snapshots s USING(space_id,version) WHERE h.space_id=$1 AND h.version=3 AND s.sealed) AND (SELECT count(*) FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=3)=13`

var errDatabase = errors.New("database unavailable or configuration conflicts; check migration ledger, runtime backup and doctor")

func openDB(ctx context.Context) (*sql.DB, error) {
	u := os.Getenv("MEMORYS_DATABASE_URL")
	if u == "" {
		return nil, errors.New("set MEMORYS_DATABASE_URL; supply password through PGPASSWORD or pgpass")
	}
	db, err := sql.Open("pgx", u)
	if err != nil {
		return nil, errDatabase
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if db.PingContext(c) != nil {
		db.Close()
		return nil, errDatabase
	}
	return db, nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type configRow struct {
	insert, check string
	args          []any
}

func (s *state) rows() []configRow {
	m := s.m
	policy := document.ReviewPolicySnapshot{Version: 1, AcceptedDecisionPolicyVersions: []uint64{1}}
	policy.Digest, _ = document.ReviewPolicyDigest(policy)
	backend := document.PublicationBackendSnapshot{EpochID: "local-backend", Number: 1, CutoverWatermark: "local-initial", IndexProfile: contracts.ProfileReference{ID: s.profile.Index.ID, Version: 1}, ProfileDigest: s.profile.Index.Digest, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}}
	profile, _ := encodeProfile(s)
	return []configRow{
		{"INSERT INTO akos_withdraw_v1.identity_issuers VALUES($1,true)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.identity_issuers WHERE issuer=$1 AND enabled)", []any{m.Issuer}},
		{"INSERT INTO akos_withdraw_v1.identity_signing_keys VALUES($1,$2,$3,true,$4,$5)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.identity_signing_keys WHERE issuer=$1 AND key_id=$2 AND public_key_digest=$3 AND enabled AND valid_from=$4 AND valid_until=$5)", []any{m.Issuer, m.KeyID, m.PublicKeyDigest, m.KeyFrom, m.KeyUntil}},
		{"INSERT INTO akos_withdraw_v1.identity_subjects VALUES($1,$2,$3,true)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.identity_subjects WHERE issuer=$1 AND subject_id=$2 AND subject_type=$3 AND enabled)", []any{m.Issuer, m.Owner, "user"}},
		{"INSERT INTO akos_withdraw_v1.identity_subjects VALUES($1,$2,$3,true)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.identity_subjects WHERE issuer=$1 AND subject_id=$2 AND subject_type=$3 AND enabled)", []any{m.Issuer, m.Worker, "service"}},
		{"INSERT INTO akos_withdraw_v1.identity_clients VALUES($1,$2,true)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.identity_clients WHERE issuer=$1 AND client_id=$2 AND enabled)", []any{m.Issuer, m.Client}},
		{"INSERT INTO akos_withdraw_v1.review_policy_snapshots VALUES($1,$2,1,$3,$4::jsonb)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.review_policy_snapshots WHERE space_id=$1 AND kb_id=$2 AND version=1 AND digest=$3 AND payload=$4::jsonb)", []any{m.Space, m.KB, policy.Digest, string(encode(policy))}},
		{"INSERT INTO akos_withdraw_v1.review_policy_heads VALUES($1,$2,1,1)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.review_policy_heads WHERE space_id=$1 AND kb_id=$2 AND policy_version=1 AND row_version=1)", []any{m.Space, m.KB}},
		{"INSERT INTO akos_withdraw_v1.publication_backend_epochs VALUES($1,$2,'local-backend',$3::jsonb)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_backend_epochs WHERE space_id=$1 AND kb_id=$2 AND id='local-backend' AND payload=$3::jsonb)", []any{m.Space, m.KB, string(encode(backend))}},
		{"INSERT INTO akos_withdraw_v1.publication_backend_heads VALUES($1,$2,'local-backend',1)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_backend_heads WHERE space_id=$1 AND kb_id=$2 AND backend_epoch_id='local-backend' AND row_version=1)", []any{m.Space, m.KB}},
		{"INSERT INTO akos_withdraw_v1.query_profile_catalog VALUES($1,$2)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.query_profile_catalog WHERE profile_digest=$1 AND payload=$2)", []any{m.ProfileDigest, profile}},
		{"INSERT INTO akos_withdraw_v1.query_profile_bindings VALUES($1,$2,1,$3)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.query_profile_bindings WHERE space_id=$1 AND kb_id=$2 AND version=1 AND profile_digest=$3)", []any{m.Space, m.KB, m.ProfileDigest}},
		{"INSERT INTO akos_withdraw_v1.query_profile_heads VALUES($1,$2,1)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.query_profile_heads WHERE space_id=$1 AND kb_id=$2 AND version=1)", []any{m.Space, m.KB}},
		{"INSERT INTO akos_withdraw_v1.query_store_head VALUES(true,$1,1)", "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.query_store_head WHERE singleton AND store_epoch_id=$1 AND row_version=1)", []any{m.Store}},
	}
}

func ownerActions() []string {
	return []string{"document.create", "document.submit", "revision.approve", "publication.start", "publication.publish", "publication.withdraw", "document.delete", "knowledge.search", "publication.cleanup.read", "publication.cleanup.control"}
}
func workerActions() []string {
	return []string{"publication.prepare", "publication.cleanup.execute", "index.delete"}
}
func (s *state) grants() []configRow {
	var rows []configRow
	actions := ownerActions()
	if s.m.Features == 1 {
		actions = append(actions, "document.parse", "original.read", "original.write", "knowledge.answer")
	}
	for _, pair := range []struct {
		subject contracts.ID
		actions []string
	}{{s.m.Owner, actions}, {s.m.Worker, workerActions()}} {
		for _, a := range pair.actions {
			rows = append(rows, configRow{fmt.Sprintf("INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,%d,$2,$3,$4,$5,$6)", s.aclVersion()), fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=%d AND issuer=$2 AND subject_id=$3 AND client_id=$4 AND kb_id=$5 AND action=$6)", s.aclVersion()), []any{s.m.Space, s.m.Issuer, pair.subject, s.m.Client, s.m.KB, a}})
		}
	}
	return rows
}
func (s *state) aclVersion() uint64 {
	if s.m.Features == 1 {
		return 4
	}
	return 3
}
func (s *state) aclCheck() string {
	if s.m.Features == 0 {
		return localACLCheck
	}
	return `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.access_policy_heads h JOIN akos_withdraw_v1.access_policy_snapshots s USING(space_id,version) WHERE h.space_id=$1 AND h.version=4 AND s.sealed) AND (SELECT count(*) FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=4)=17`
}

func (s *state) migrateACL(ctx context.Context, tx *sql.Tx) error {
	var current uint64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM akos_withdraw_v1.access_policy_heads WHERE space_id=$1 FOR UPDATE`, s.m.Space).Scan(&current); err != nil {
		return errDatabase
	}
	target := s.aclVersion()
	if current >= target {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots(space_id,version,sealed) VALUES($1,$2,false) ON CONFLICT(space_id,version) DO NOTHING`, s.m.Space, target); err != nil {
		return errDatabase
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action) SELECT space_id,$2,issuer,subject_id,client_id,kb_id,action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=$3 ON CONFLICT DO NOTHING`, s.m.Space, target, current); err != nil {
		return errDatabase
	}
	// Re-assert the complete local grant set at the target version. This keeps
	// upgrades from older snapshots correct when optional AI grants are present,
	// while the unique key makes the copy idempotent.
	for _, grant := range s.grants() {
		if _, err := tx.ExecContext(ctx, grant.insert, grant.args...); err != nil {
			return errDatabase
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=$2`, s.m.Space, target); err != nil {
		return errDatabase
	}
	if _, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=$2 WHERE space_id=$1 AND version=$3`, s.m.Space, target, current); err != nil {
		return errDatabase
	}
	return nil
}

func checkSchema(ctx context.Context, q queryer) error {
	var got string
	if q.QueryRowContext(ctx, "SELECT checksum FROM public.memorys_schema_migrations WHERE version='withdraw_contract_v1'").Scan(&got) != nil || got != schemaChecksum {
		return errors.New("schema migration ledger missing or mismatched; run memorys-migrate against the intended database")
	}
	return nil
}

func (s *state) validateDB(ctx context.Context, q queryer) error {
	if checkSchema(ctx, q) != nil {
		return errDatabase
	}
	for _, r := range append(s.rows(), s.grants()...) {
		var ok bool
		if q.QueryRowContext(ctx, r.check, r.args...).Scan(&ok) != nil || !ok {
			return errDatabase
		}
	}
	var ok bool
	if q.QueryRowContext(ctx, s.aclCheck(), s.m.Space).Scan(&ok) != nil || !ok {
		return errDatabase
	}
	return nil
}

func (s *state) initialize(ctx context.Context, db *sql.DB) error {
	if checkSchema(ctx, db) != nil {
		return errors.New("apply the verified withdraw_contract_v1 migration first with memorys-migrate")
	}
	if err := applyPauseMigrations(ctx, db); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return errDatabase
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext('memorys:local-initialize'))"); err != nil {
		return errDatabase
	}
	var count int
	if tx.QueryRowContext(ctx, "SELECT count(*) FROM akos_withdraw_v1.query_store_head").Scan(&count) != nil {
		return errDatabase
	}
	if count == 0 {
		if s.m.Ready {
			return errDatabase
		}
		if tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM akos_withdraw_v1.documents)+(SELECT count(*) FROM akos_withdraw_v1.identity_issuers)+(SELECT count(*) FROM akos_withdraw_v1.query_profile_catalog)+(SELECT count(*) FROM akos_withdraw_v1.access_policy_heads)`).Scan(&count) != nil || count != 0 {
			return errDatabase
		}
		for _, r := range s.rows() {
			if _, err = tx.ExecContext(ctx, r.insert, r.args...); err != nil {
				return errDatabase
			}
		}
		target := s.aclVersion()
		if _, err = tx.ExecContext(ctx, "INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,$2,false)", s.m.Space, target); err != nil {
			return errDatabase
		}
		for _, r := range s.grants() {
			if _, err = tx.ExecContext(ctx, r.insert, r.args...); err != nil {
				return errDatabase
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=$2", s.m.Space, target); err != nil {
			return errDatabase
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO akos_withdraw_v1.access_policy_heads VALUES($1,$2)", s.m.Space, target); err != nil {
			return errDatabase
		}
	} else if err = s.migrateACL(ctx, tx); err != nil {
		return err
	}
	if err = s.validateDB(ctx, tx); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return errDatabase
	}
	s.m.Ready = true
	if writeAtomic(s.root, "manifest.json", encode(s.m)) != nil {
		return errState
	}
	return nil
}

// applyPauseMigrations upgrades only task tables that are already installed.
// This keeps a plain text-only local database compatible while making the
// pause controls usable automatically after AI/management task migrations.
func applyPauseMigrations(ctx context.Context, db *sql.DB) error {
	if ctx == nil || db == nil {
		return errDatabase
	}
	migrations := []struct {
		version, file, relation string
	}{
		{"publication_tasks_v2", "docs/architecture/sql/publication_tasks_v2.sql", "akos_withdraw_v1.publication_task_heads"},
		{"parse_tasks_v3", "docs/architecture/sql/parse_tasks_v3.sql", "akos_withdraw_v1.parse_task_heads"},
	}
	for _, item := range migrations {
		var installed bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, item.relation).Scan(&installed); err != nil {
			return errDatabase
		}
		if !installed {
			continue
		}
		source, err := os.ReadFile(item.file)
		if err != nil {
			return errDatabase
		}
		if _, err := migration.Apply(ctx, db, item.version, source); err != nil {
			return errDatabase
		}
	}
	return nil
}

// Raw canonical bytes are retained in the catalog; JSONB is only used for
// governance snapshots whose structural equality is checked by PostgreSQL.
func encodeProfile(s *state) ([]byte, error) { return profileBytes(s.profile) }

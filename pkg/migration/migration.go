// Package migration provides a small, transaction-safe migration ledger for
// local and hosted PostgreSQL deployments. Migration files are treated as
// immutable inputs and are applied at most once per version and checksum.
package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const ledgerTable = "public.memorys_schema_migrations"

var (
	ErrInvalidMigration = errors.New("invalid migration")
	ErrUntrackedSchema  = errors.New("migration schema already exists without a ledger entry")
	ErrChecksumMismatch = errors.New("migration checksum mismatch")
)

type Result struct {
	Version  string
	Checksum string
	Applied  bool
}

// Apply applies one migration to db. The migration must contain a single
// BEGIN/COMMIT envelope. The envelope is moved into the database/sql
// transaction so the ledger row and schema changes commit atomically.
func Apply(ctx context.Context, db *sql.DB, version string, source []byte) (Result, error) {
	var result Result
	if ctx == nil || db == nil || strings.TrimSpace(version) == "" || len(source) == 0 {
		return result, ErrInvalidMigration
	}
	body, err := transactionBody(string(source))
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(source)
	checksum := hex.EncodeToString(sum[:])
	result = Result{Version: version, Checksum: checksum}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS public.memorys_schema_migrations (
		version text PRIMARY KEY,
		checksum text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
		applied_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "memorys:migrations"); err != nil {
		return Result{}, err
	}
	var recorded string
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM public.memorys_schema_migrations WHERE version=$1`, version).Scan(&recorded)
	switch {
	case err == nil:
		if recorded != checksum {
			return Result{}, fmt.Errorf("%w: version %s", ErrChecksumMismatch, version)
		}
		if err = tx.Commit(); err != nil {
			return Result{}, err
		}
		result.Applied = false
		return result, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Result{}, err
	}
	var schemaExists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname='akos_withdraw_v1')`).Scan(&schemaExists); err != nil {
		return Result{}, err
	}
	if schemaExists {
		return Result{}, ErrUntrackedSchema
	}
	if _, err = tx.ExecContext(ctx, body); err != nil {
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.memorys_schema_migrations(version, checksum) VALUES($1,$2)`, version, checksum); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	result.Applied = true
	return result, nil
}

func transactionBody(source string) (string, error) {
	trimmed := strings.TrimSpace(source)
	upper := strings.ToUpper(trimmed)
	begin := strings.Index(upper, "BEGIN;")
	commit := strings.LastIndex(upper, "COMMIT;")
	if begin < 0 || commit <= begin+len("BEGIN;") || strings.TrimSpace(trimmed[commit+len("COMMIT;"):]) != "" {
		return "", ErrInvalidMigration
	}
	return strings.TrimSpace(trimmed[begin+len("BEGIN;") : commit]), nil
}

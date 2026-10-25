package localcli

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modelclient"
	"github.com/MemoryGet/memorys/pkg/parserprocess"
)

func ollamaURL() string {
	if v := os.Getenv("MEMORYS_OLLAMA_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:11434"
}
func converter() (parserprocess.MarkItDown, error) {
	python := os.Getenv("MEMORYS_PARSER_PYTHON")
	if python == "" {
		python = filepath.Join(".local", "markitdown-env", "Scripts", "python.exe")
	}
	worker := os.Getenv("MEMORYS_PARSER_WORKER")
	if worker == "" {
		worker = filepath.Join("tools", "parsers", "markitdown_worker.py")
	}
	python, err := filepath.Abs(python)
	if err != nil {
		return parserprocess.MarkItDown{}, errState
	}
	worker, err = filepath.Abs(worker)
	if err != nil {
		return parserprocess.MarkItDown{}, errState
	}
	return parserprocess.MarkItDown{Python: python, Script: worker}, nil
}

// enableAI explicitly advances the sealed local policy. A DB-committed,
// file-pending upgrade can be reconciled; unrelated/revoked policies cannot.
func (s *state) enableAI(ctx context.Context, db *sql.DB) error {
	p, err := converter()
	if err != nil || p.Health(ctx) != nil {
		return errors.New("pinned office parser unavailable; configure MEMORYS_PARSER_PYTHON and MEMORYS_PARSER_WORKER")
	}
	model := os.Getenv("MEMORYS_EMBEDDING_MODEL")
	if model == "" {
		model = "embeddinggemma:latest"
	}
	embedding, err := modelclient.DiscoverOllama(ctx, ollamaURL(), model)
	if err != nil {
		return errors.New("local embedding model unavailable; pull embeddinggemma in Ollama or configure MEMORYS_EMBEDDING_MODEL")
	}
	spec := embedding.Spec()
	return s.upgradeAI(ctx, db, spec)
}

func (s *state) upgradeAI(ctx context.Context, db *sql.DB, spec contracts.EmbeddingSpec) error {
	if _, err := modelclient.NewOllama(ollamaURL(), spec); err != nil {
		return err
	}
	if s.m.Features == 1 {
		if !reflect.DeepEqual(s.m.Embedding, &spec) {
			return errors.New("pinned embedding model changed; restore its original digest before continuing")
		}
		return s.validateDB(ctx, db)
	}
	owner, err := s.identity(ctx, db, false)
	if err != nil {
		return err
	}
	defer owner.close()
	if _, err = owner.scope(ctx, "document.create", ""); err != nil {
		return errAuthority
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return errDatabase
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext('memorys:local-initialize'))"); err != nil {
		return errDatabase
	}
	// Capability changes start with an empty optional vector memo; never copy
	// synchronization state or reuse a prior embedding namespace.
	upgraded := &state{root: s.root, m: s.m, profile: s.profile, key: s.key}
	upgraded.m.Features = 1
	upgraded.m.Embedding = &spec
	var version int
	if tx.QueryRowContext(ctx, "SELECT version FROM akos_withdraw_v1.access_policy_heads WHERE space_id=$1 FOR UPDATE", s.m.Space).Scan(&version) != nil {
		return errDatabase
	}
	if version == 1 {
		if s.validateDB(ctx, tx) != nil {
			return errDatabase
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,2,false)", s.m.Space); err != nil {
			return errDatabase
		}
		for _, r := range upgraded.grants() {
			if _, err = tx.ExecContext(ctx, r.insert, r.args...); err != nil {
				return errDatabase
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=2", s.m.Space); err != nil {
			return errDatabase
		}
		if _, err = tx.ExecContext(ctx, "UPDATE akos_withdraw_v1.access_policy_heads SET version=2 WHERE space_id=$1 AND version=1", s.m.Space); err != nil {
			return errDatabase
		}
	} else if version != 2 {
		return errDatabase
	}
	if upgraded.validateDB(ctx, tx) != nil {
		return errDatabase
	}
	if _, err = owner.scope(ctx, "document.create", ""); err != nil {
		return errAuthority
	}
	if tx.Commit() != nil {
		return errors.New("capability upgrade outcome uncertain; retry enable-ai with the same runtime directory")
	}
	if writeAtomic(s.root, "manifest.json", encode(upgraded.m)) != nil {
		return errState
	}
	s.m = upgraded.m
	return nil
}

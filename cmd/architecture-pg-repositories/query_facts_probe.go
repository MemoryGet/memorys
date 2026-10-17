package main

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type countedQueryFacts func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error)

func (f countedQueryFacts) LoadCurrentFacts(c context.Context, r contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
	return f(c, r, s)
}

var queryFactsSamples []map[string]any

func referenceQuerySnapshot(v uint64) (configprofile.Snapshot, error) {
	index, err := referenceIndexProfile(1)
	if err != nil {
		return configprofile.Snapshot{}, err
	}
	binding := contracts.ModuleBinding{InstanceID: "postgres-keyword-reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	return configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "native-pipeline", Version: v, IndexProfile: contracts.ProfileReference{ID: index.ID, Version: index.Version}, QueryProfile: contracts.ProfileReference{ID: "native-query", Version: v}}, Index: index, Query: contracts.QueryProfile{ID: "native-query", Version: v, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: binding}, Routes: []contracts.RetrievalRoute{{ID: "keyword-route", Binding: binding, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 1000, Required: true}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 1000, AnswerEvidenceLimit: 10, FusionMethod: "rrf", RankConstant: 60 + int(v), RetrievalTimeoutMS: 5000}}})
}

func nativeQueryFacts(ctx context.Context, db *sql.DB, r contracts.RequestContext, s contracts.AccessScope) (postgres.QueryFacts, func() error, error) {
	p := postgres.QueryFacts{DB: db}
	snapshot, err := referenceQuerySnapshot(1)
	if err != nil {
		return p, nil, err
	}
	payload, err := configprofile.Encode(snapshot)
	if err != nil || snapshot.Pipeline.Digest != r.ProfileDigest {
		return p, nil, errors.New("request is not bound to actual profile")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_catalog VALUES($1,$2)`, snapshot.Pipeline.Digest, payload); err != nil {
		return p, nil, err
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_bindings VALUES($1,$2,1,$3)`, s.SpaceID, s.KnowledgeBaseIDs[0], r.ProfileDigest); err != nil {
		return p, nil, err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_heads VALUES($1,$2,1)`, s.SpaceID, s.KnowledgeBaseIDs[0]); err != nil {
		return p, nil, err
	}
	got, err := p.LoadCurrentFacts(ctx, r, s)
	if err != nil || got.ProfileDigest != r.ProfileDigest || got.ReviewPolicies[s.KnowledgeBaseIDs[0]] == 0 {
		return p, nil, errors.New("native query facts missing")
	}
	requested := r
	requested.ProfileDigest = contracts.Digest(strings.Repeat("f", 64))
	still, err := p.LoadCurrentFacts(ctx, requested, s)
	if err != nil || still.ProfileDigest != got.ProfileDigest {
		return p, nil, errors.New("query facts echoed request digest")
	}
	bad := s
	bad.KnowledgeBaseIDs = []contracts.ID{"missing-kb"}
	if _, err := p.LoadCurrentFacts(ctx, r, bad); err == nil {
		return p, nil, errors.New("missing query binding accepted")
	}
	bad = s
	bad.PolicyVersion++
	if _, err := p.LoadCurrentFacts(ctx, r, bad); err == nil {
		return p, nil, errors.New("stale authorization version accepted by facts")
	}
	var stored contracts.Digest
	if err := db.QueryRowContext(ctx, `SELECT b.profile_digest FROM akos_withdraw_v1.query_profile_heads h JOIN akos_withdraw_v1.query_profile_bindings b USING(space_id,kb_id,version) WHERE h.space_id=$1 AND h.kb_id=$2`, s.SpaceID, s.KnowledgeBaseIDs[0]).Scan(&stored); err != nil {
		return p, nil, err
	}
	queryFactsSamples = append(queryFactsSamples, map[string]any{"request": r, "scope": s, "facts": got, "stored_digest": stored, "scope_kind": "verified-profile-snapshot", "profile_payload": payload})
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.query_profile_bindings SET profile_digest=$3 WHERE space_id=$1 AND kb_id=$2`, s.SpaceID, s.KnowledgeBaseIDs[0], requested.ProfileDigest); err == nil {
		return p, nil, errors.New("immutable profile binding changed")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.query_profile_catalog SET payload=$2 WHERE profile_digest=$1`, snapshot.Pipeline.Digest, payload); err == nil {
		return p, nil, errors.New("profile catalog mutable")
	}
	changed, err := referenceQuerySnapshot(2)
	if err != nil {
		return p, nil, err
	}
	nextPayload, err := configprofile.Encode(changed)
	if err != nil {
		return p, nil, err
	}
	change := func() error {
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_catalog VALUES($1,$2)`, requested.ProfileDigest, payload); err != nil {
			return err
		}
		if err := activateNativeProfile(ctx, db, s.SpaceID, s.KnowledgeBaseIDs[0], requested.ProfileDigest); err != nil {
			return err
		}
		if _, err := p.LoadCurrentFacts(ctx, r, s); err == nil {
			return errors.New("mismatched catalog payload accepted")
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_catalog VALUES($1,$2)`, changed.Pipeline.Digest, nextPayload); err != nil {
			return err
		}
		if err := activateNativeProfile(ctx, db, s.SpaceID, s.KnowledgeBaseIDs[0], changed.Pipeline.Digest); err != nil {
			return err
		}
		actual, err := p.LoadCurrentFacts(ctx, r, s)
		if err != nil || actual.ProfileDigest != changed.Pipeline.Digest {
			return errors.New("current profile switch invisible")
		}
		if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.query_profile_heads SET version=1 WHERE space_id=$1 AND kb_id=$2`, s.SpaceID, s.KnowledgeBaseIDs[0]); err == nil {
			return errors.New("profile head rollback accepted")
		}
		return nil
	}

	return p, change, nil
}

func activateNativeProfile(ctx context.Context, db *sql.DB, space, kb contracts.ID, digest contracts.Digest) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT version::text FROM akos_withdraw_v1.query_profile_heads WHERE space_id=$1 AND kb_id=$2 FOR UPDATE`, space, kb).Scan(&raw); err != nil {
		return err
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == ^uint64(0) {
		return errors.New("invalid configuration generation")
	}
	next := strconv.FormatUint(n+1, 10)
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_bindings VALUES($1,$2,$3,$4)`, space, kb, next, digest); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.query_profile_heads SET version=$3 WHERE space_id=$1 AND kb_id=$2`, space, kb, next); err != nil {
		return err
	}
	return tx.Commit()
}

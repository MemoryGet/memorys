package main

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var authorizationSamples []map[string]any

func nativePolicyAuthorizer(ctx context.Context, db *sql.DB, scope contracts.AccessScope, who contracts.Principal, document contracts.ID) (postgres.Authorizer, func() error, error) {
	p := postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}
	version := strconv.FormatUint(scope.PolicyVersion, 10)
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,$2,false)`, []any{scope.SpaceID, version}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,$4,$5,$6,$7)`, []any{scope.SpaceID, version, who.Issuer, who.SubjectID, who.ClientID, scope.KnowledgeBaseIDs[0], scope.Action}},
		{`INSERT INTO akos_withdraw_v1.identity_subjects VALUES($1,'read-only-fixture','user',true)`, []any{who.Issuer}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,'read-only-fixture',$4,$5,'knowledge.read')`, []any{scope.SpaceID, version, who.Issuer, who.ClientID, scope.KnowledgeBaseIDs[0]}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,$4,$5,$6,'knowledge.read')`, []any{scope.SpaceID, version, who.Issuer, who.SubjectID, who.ClientID, scope.KnowledgeBaseIDs[0]}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,$4,$5,$6,'knowledge.history.read')`, []any{scope.SpaceID, version, who.Issuer, who.SubjectID, who.ClientID, scope.KnowledgeBaseIDs[0]}},
		{`INSERT INTO akos_withdraw_v1.identity_subjects VALUES($1,'search-only-fixture','user',true)`, []any{who.Issuer}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,'search-only-fixture',$4,$5,'knowledge.search')`, []any{scope.SpaceID, version, who.Issuer, who.ClientID, scope.KnowledgeBaseIDs[0]}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,'historical-second-reader',$4,$5,'knowledge.read')`, []any{scope.SpaceID, version, who.Issuer, who.ClientID, scope.KnowledgeBaseIDs[0]}},
		{`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,'historical-second-reader',$4,$5,'knowledge.history.read')`, []any{scope.SpaceID, version, who.Issuer, who.ClientID, scope.KnowledgeBaseIDs[0]}},
		{`UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=$2`, []any{scope.SpaceID, version}},
		{`INSERT INTO akos_withdraw_v1.access_policy_heads VALUES($1,$2)`, []any{scope.SpaceID, version}},
	} {
		if _, err := db.ExecContext(ctx, s.sql, s.args...); err != nil {
			return p, nil, err
		}
	}
	q := contracts.AuthorizeRequest{Principal: who, Action: scope.Action, SpaceID: scope.SpaceID, KnowledgeBaseIDs: scope.KnowledgeBaseIDs, ResourceIDs: []contracts.ID{document}}
	out, err := p.Authorize(ctx, q)
	if err != nil || out.PolicyVersion != scope.PolicyVersion {
		return p, nil, errors.New("native ACL grant denied")
	}
	readActor := who
	readActor.SubjectID = "read-only-fixture"
	location, locateErr := p.LocateReadableDocument(ctx, readActor, document)
	if locateErr != nil || location.DocumentID != document || location.SpaceID != scope.SpaceID || location.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] || location.Scope.Action != "knowledge.read" {
		return p, nil, errors.New("read-only document location denied or ownership differs")
	}
	searchActor := who
	searchActor.SubjectID = "search-only-fixture"
	if _, e := p.LocateReadableDocument(ctx, searchActor, document); e != postgres.ErrAuthorization {
		return p, nil, errors.New("search-only actor located readable document")
	}
	if _, e := p.LocateReadableDocument(ctx, readActor, "missing-document"); e != postgres.ErrAuthorization {
		return p, nil, errors.New("missing document error differs")
	}
	readQuery := q
	readQuery.Principal = readActor
	if _, e := p.Authorize(ctx, readQuery); e != postgres.ErrAuthorization {
		return p, nil, errors.New("read grant enabled search")
	}
	historyQuery := readQuery
	historyQuery.Action = contracts.HistoricalReadAction
	if _, e := p.Authorize(ctx, historyQuery); e != postgres.ErrAuthorization {
		return p, nil, errors.New("current read grant enabled history")
	}
	readUnit := postgres.QueryReads{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		fresh, e := p.Authorize(c, contracts.AuthorizeRequest{Principal: readActor, Action: s.Action, SpaceID: s.SpaceID, KnowledgeBaseIDs: s.KnowledgeBaseIDs, ResourceIDs: []contracts.ID{document}})
		if e != nil || fresh.PolicyVersion != s.PolicyVersion {
			return postgres.ErrAuthorization
		}
		return nil
	}}
	if e := readUnit.WithinRead(ctx, location.Scope, func(c context.Context, _ contracts.QueryReadIdentity, r contracts.QueryReadRepository) error {
		state, e := r.LoadCurrentDocument(c, document)
		if e != nil || state.Document.ID != document {
			return errors.New("read-only metadata unavailable")
		}
		return nil
	}); e != nil {
		return p, nil, e
	}
	listed, err := p.ListKnowledgeBases(ctx, who, scope.SpaceID)
	if err != nil || len(listed) != 1 || listed[0] != scope.KnowledgeBaseIDs[0] {
		return p, nil, errors.New("current KB directory differs")
	}
	if _, err := p.ListKnowledgeBases(ctx, who, "missing-space"); err == nil {
		return p, nil, errors.New("missing space policy treated as empty grants")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_subjects SET enabled=false WHERE issuer=$1 AND subject_id=$2`, who.Issuer, who.SubjectID); err != nil {
		return p, nil, err
	}
	disabled, disabledErr := p.ListKnowledgeBases(ctx, who, scope.SpaceID)
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_subjects SET enabled=true WHERE issuer=$1 AND subject_id=$2`, who.Issuer, who.SubjectID); err != nil {
		return p, nil, err
	}
	if disabledErr == nil || disabled != nil {
		return p, nil, errors.New("disabled identity treated as empty grants")
	}
	for _, change := range []func(*contracts.AuthorizeRequest){
		func(r *contracts.AuthorizeRequest) { r.Action = "knowledge.delete" },
		func(r *contracts.AuthorizeRequest) { r.KnowledgeBaseIDs = []contracts.ID{"other-kb"} },
		func(r *contracts.AuthorizeRequest) { r.KnowledgeBaseIDs = nil },
		func(r *contracts.AuthorizeRequest) { r.SpaceID = "other-space" },
		func(r *contracts.AuthorizeRequest) { r.ResourceIDs = []contracts.ID{"missing-document"} },
		func(r *contracts.AuthorizeRequest) { r.Principal.ClientID = "other-client" },
		func(r *contracts.AuthorizeRequest) { r.Principal.Issuer = "other-issuer" },
		func(r *contracts.AuthorizeRequest) {
			r.Principal.Delegation = &contracts.Delegation{UserID: "reader", ClientID: "identity-client"}
		},
	} {
		bad := q
		change(&bad)
		if _, err := p.Authorize(ctx, bad); err == nil {
			return p, nil, errors.New("ungranted native ACL request allowed")
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants VALUES($1,$2,$3,$4,$5,$6,'knowledge.delete')`, scope.SpaceID, version, who.Issuer, who.SubjectID, who.ClientID, scope.KnowledgeBaseIDs[0]); err == nil {
		return p, nil, errors.New("sealed ACL accepted late grant")
	}
	var storedSpace, storedVersion, storedIssuer, storedSubject, storedClient, storedKB, storedAction string
	if err := db.QueryRowContext(ctx, `SELECT space_id,version::text,issuer,subject_id,client_id,kb_id,action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=$2 AND action='knowledge.search' AND subject_id=$3`, scope.SpaceID, version, who.SubjectID).Scan(&storedSpace, &storedVersion, &storedIssuer, &storedSubject, &storedClient, &storedKB, &storedAction); err != nil {
		return p, nil, err
	}
	authorizationSamples = append(authorizationSamples, map[string]any{"request": q, "scope": out, "listed_knowledge_bases": listed, "stored_grant": map[string]string{"space": storedSpace, "version": storedVersion, "issuer": storedIssuer, "subject": storedSubject, "client": storedClient, "kb": storedKB, "action": storedAction}})
	revoke := func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		next := strconv.FormatUint(scope.PolicyVersion+1, 10)
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,$2,true)`, scope.SpaceID, next); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=$2 WHERE space_id=$1`, scope.SpaceID, next); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		empty, err := p.ListKnowledgeBases(ctx, who, scope.SpaceID)
		if err != nil || len(empty) != 0 {
			return errors.New("legitimate empty policy directory failed")
		}
		if _, err = p.Authorize(ctx, q); err == nil {
			return errors.New("revoked native ACL accepted")
		}
		if _, err = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=$2 WHERE space_id=$1`, scope.SpaceID, version); err == nil {
			return errors.New("ACL head rollback accepted")
		}
		return nil
	}
	return p, revoke, nil
}

package localruntime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// NewParseWorker wires the durable PostgreSQL parse queue, candidate
// repository, current authorization and a host-selected parser into the
// server-owned worker lifecycle. One runtime is intentionally bound to one
// space/KB partition; hosts with multiple partitions create one per partition.
func (s *Search) NewParseWorker(ctx context.Context, parser contracts.Parser, space, knowledgeBase, workerID contracts.ID, interval time.Duration, onError func(error)) (*ingestion.ParseWorkerRuntime, error) {
	return s.newParseWorker(ctx, parser, space, knowledgeBase, workerID, interval, onError, false)
}

// NewParseWorkerAll consumes queued parse tasks across every current
// space/knowledge-base partition. Each command is still authorized against
// its own stored scope before parsing and candidate commit.
func (s *Search) NewParseWorkerAll(ctx context.Context, parser contracts.Parser, workerID contracts.ID, interval time.Duration, onError func(error)) (*ingestion.ParseWorkerRuntime, error) {
	return s.newParseWorker(ctx, parser, "", "", workerID, interval, onError, true)
}

func (s *Search) newParseWorker(ctx context.Context, parser contracts.Parser, space, knowledgeBase, workerID contracts.ID, interval time.Duration, onError func(error), global bool) (*ingestion.ParseWorkerRuntime, error) {
	if s == nil || s.db == nil || s.auth == nil || s.blobs == nil || parser == nil || ctx == nil || ctx.Err() != nil || !global && (!validParseWorkerID(space) || !validParseWorkerID(knowledgeBase)) || !validParseWorkerID(workerID) || interval < time.Millisecond || interval > time.Minute {
		return nil, errors.New("parse worker unavailable")
	}
	_, profileDigest, err := s.ProfileIdentity()
	if err != nil {
		return nil, errors.New("parse worker profile unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	verifyScope := func(_ context.Context, scope contracts.AccessScope) error {
		if scope.Action != "document.parse" || len(scope.KnowledgeBaseIDs) != 1 || scope.PolicyVersion == 0 || !scope.ExpiresAt.After(time.Now()) || !global && (scope.SpaceID != space || scope.KnowledgeBaseIDs[0] != knowledgeBase) {
			return errors.New("parse worker scope rejected")
		}
		return nil
	}
	authorizeCandidate := func(c context.Context, command document.CandidateCommand) error {
		if len(command.Scope.KnowledgeBaseIDs) != 1 || command.Scope.SubjectID == "" || !global && (command.Scope.SpaceID != space || command.Scope.KnowledgeBaseIDs[0] != knowledgeBase) {
			return errors.New("parse candidate scope rejected")
		}
		var allowed bool
		if err := s.db.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.documents d JOIN akos_withdraw_v1.access_policy_heads h ON h.space_id=d.space_id AND h.version=$2::numeric JOIN akos_withdraw_v1.access_policy_grants g ON g.space_id=h.space_id AND g.version=h.version AND g.subject_id=$3 AND g.kb_id=d.kb_id AND g.action='document.parse' WHERE d.space_id=$1 AND d.kb_id=$4 AND d.id=$5 AND NOT d.deleted)`, string(command.Scope.SpaceID), command.Scope.PolicyVersion, string(command.Scope.SubjectID), string(command.Scope.KnowledgeBaseIDs[0]), string(command.DocumentID)).Scan(&allowed); err != nil || !allowed {
			return errors.New("parse candidate authorization rejected")
		}
		return nil
	}
	service := ingestion.CandidateService{Parser: parser, Repository: postgres.Candidates{DB: s.db, Verify: verifyScope, AuthorizeCandidate: authorizeCandidate}, ProfileDigest: profileDigest, Authorize: func(c context.Context, command ingestion.DraftParseCommand) error {
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: command.Principal, Action: "document.parse", SpaceID: command.Scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), command.Scope.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{command.DocumentID}})
		if authErr != nil || queryread.MatchRefreshedScope(c, command.Principal, command.Scope, command.Principal, fresh, command.Scope.KnowledgeBaseIDs) != nil {
			return errors.New("parse authorization rejected")
		}
		return nil
	}}
	var queue ingestion.ParseTaskQueue
	if global {
		queue = postgres.GlobalParseTaskQueue{Repository: postgres.ParseTaskRepository{DB: s.db}, WorkerID: workerID, LeaseDuration: 30 * time.Second}
	} else {
		queue = postgres.BoundParseTaskQueue{Repository: postgres.ParseTaskRepository{DB: s.db}, Space: space, KnowledgeBase: knowledgeBase, WorkerID: workerID, LeaseDuration: 30 * time.Second}
	}
	return ingestion.NewParseWorkerRuntime(ingestion.ParseWorker{Queue: queue, Decode: ingestion.DecodeParseTaskPayload, Service: service}, interval, onError)
}

func validParseWorkerID(value contracts.ID) bool {
	return value != "" && len(value) <= 200 && !strings.ContainsAny(string(value), "\x00\r\n")
}

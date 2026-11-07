package localruntime

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/chunk"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/preparation"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// NewPublicationWorker assembles the durable PostgreSQL preparation queue and
// index writer for one space/knowledge-base partition. The worker credential
// is copied into the lifecycle closure and reauthenticated for every queue,
// task and publisher authorization check.
func (s *Search) NewPublicationWorker(ctx context.Context, credential contracts.Credential, space, knowledgeBase, workerID contracts.ID, interval time.Duration, onError func(error)) (*preparation.Service, error) {
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || ctx.Err() != nil || credential.Kind != "access_token" || len(credential.Secret) == 0 || !validParseWorkerID(space) || !validParseWorkerID(knowledgeBase) || !validParseWorkerID(workerID) || interval < time.Millisecond || interval > time.Minute {
		return nil, errors.New("publication worker unavailable")
	}
	profile, indexProfile, err := s.publicationProfile()
	if err != nil {
		return nil, errors.New("publication index profile unavailable")
	}
	profileDigest := indexProfile.Digest
	if profile == 0 || len(profileDigest) != 64 {
		return nil, errors.New("publication worker profile unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	workerSecret := append([]byte(nil), credential.Secret...)
	workerCredential := contracts.Credential{Kind: credential.Kind, Secret: workerSecret}
	authenticate := func(c context.Context) (contracts.Principal, error) {
		secret := contracts.Credential{Kind: workerCredential.Kind, Secret: append([]byte(nil), workerCredential.Secret...)}
		defer clear(secret.Secret)
		return s.auth.Authenticate(c, secret)
	}
	scopeForWorker := func(c context.Context) (contracts.AccessScope, error) {
		who, authErr := authenticate(c)
		if authErr != nil {
			return contracts.AccessScope{}, authErr
		}
		return authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: who, Action: "publication.prepare", SpaceID: space, KnowledgeBaseIDs: []contracts.ID{knowledgeBase}})
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		who, authErr := authenticate(c)
		if authErr != nil {
			return errors.New("publication worker authentication rejected")
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: who, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...)})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, before, who, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("publication worker scope changed")
		}
		return nil
	}
	authorizeTask := func(c context.Context, access document.PublicationTaskAccess) error {
		if access.WorkerID != workerID || access.JobID == "" || access.Request.ProfileDigest != profileDigest || access.Scope.SpaceID != space || len(access.Scope.KnowledgeBaseIDs) != 1 || access.Scope.KnowledgeBaseIDs[0] != knowledgeBase {
			return errors.New("publication task binding rejected")
		}
		return verify(c, access.Scope)
	}
	authorizePublisher := func(c context.Context, plan document.PublicationStartPlan) error {
		if plan.SpaceID != space || plan.KnowledgeBaseID != knowledgeBase || plan.ProfileDigest != profileDigest || plan.OriginalPrincipal.SubjectID == "" {
			return errors.New("publication publisher binding rejected")
		}
		_, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: plan.OriginalPrincipal, Action: "publication.publish", SpaceID: plan.SpaceID, KnowledgeBaseIDs: []contracts.ID{plan.KnowledgeBaseID}, ResourceIDs: []contracts.ID{plan.Result.Operation.DocumentID}})
		return authErr
	}
	tasks := postgres.PublicationTasks{DB: s.db, Verify: verify, AuthorizeTask: authorizeTask, AuthorizePublisher: authorizePublisher}
	module := indexProfile.Bindings[contracts.IndexWriting].InstanceID
	splitter := indexProfile.Bindings[contracts.Splitting].InstanceID
	backend := postgres.PreparationIndex{Tasks: tasks, ModuleInstanceID: module}
	worker := preparation.Worker{Tasks: tasks, Splitter: chunk.SimpleSplitter{}, SplitterInstanceID: splitter, Backend: backend, ResolveProfile: func(_ context.Context, snapshot document.PublicationBackendSnapshot) (contracts.IndexProfile, error) {
		if snapshot.ProfileDigest != profileDigest || snapshot.IndexProfile.ID != indexProfile.ID || snapshot.IndexProfile.Version != indexProfile.Version {
			return contracts.IndexProfile{}, errors.New("publication profile changed")
		}
		return indexProfile, nil
	}}
	runner := preparation.NewRunner(worker, postgres.PublicationCompletion{Tasks: tasks, ModuleInstanceID: module})
	queue := postgres.PublicationQueue{DB: s.db, Scope: scopeForWorker, Space: space, KnowledgeBase: knowledgeBase, WorkerID: workerID, ProfileDigest: profileDigest, LeaseDuration: 30 * time.Second, RequestBudget: 30 * time.Second}
	return postgres.NewPublicationService(runner, queue, interval, onError)
}

// publicationProfile returns the sealed profile and its native index profile.
func (s *Search) publicationProfile() (uint64, contracts.IndexProfile, error) {
	if s == nil {
		return 0, contracts.IndexProfile{}, errors.New("publication profile unavailable")
	}
	profile, err := configprofile.Decode(s.profile)
	if err != nil {
		return 0, contracts.IndexProfile{}, err
	}
	return profile.Pipeline.Version, profile.Index, nil
}

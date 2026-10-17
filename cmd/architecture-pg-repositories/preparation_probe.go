package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/chunk"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

var preparationArtifactSamples []preparation.Artifact
var preparationSnapshotSamples []contracts.IndexSnapshot
var preparationWireSamples []json.RawMessage

func recordPreparationSample(a preparation.Artifact, s contracts.IndexSnapshot) error {
	g := a.GenerationSet.Channels[contracts.KeywordChannel]
	encoded, err := modulewire.EncodePrepareResult("preparation-proof", "preparation-invocation", modulewire.PrepareResult{Proof: *g.Readiness, Bindings: []modulewire.GenerationBinding{{GenerationID: g.ID, OpaqueBackendRef: g.BackendBinding.BackendID}}})
	if err != nil {
		return err
	}
	preparationArtifactSamples = append(preparationArtifactSamples, a)
	preparationSnapshotSamples = append(preparationSnapshotSamples, s)
	preparationWireSamples = append(preparationWireSamples, encoded)
	return nil
}

func referenceIndexProfile(version uint64) (contracts.IndexProfile, error) {
	writer := contracts.ModuleBinding{InstanceID: "postgres-keyword-preparation", Capability: contracts.IndexWriting, ContractVersion: "1.0", RequiredFeatures: []string{"attempt.isolated", "lease.fenced"}}
	splitter := contracts.ModuleBinding{InstanceID: "native-character-splitter", Capability: contracts.Splitting, ContractVersion: "1.0"}
	profile := contracts.IndexProfile{ID: "index-profile", Version: version, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: writer, contracts.Splitting: splitter}, Routes: []contracts.IndexRoute{{ID: "keyword", Channels: []contracts.Channel{contracts.KeywordChannel}, Binding: writer}}, Policy: contracts.IndexPolicy{SchemaVersion: 1, Chunking: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 2, OverlapCodepoints: 0}, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}, ProjectionFormatVersion: "akos.chunks/1"}}
	if version == 2 {
		profile.Policy.Chunking.MaxCodepoints = 3
	}
	var err error
	profile.Digest, err = preparation.ProfileDigest(profile)
	return profile, err
}

type lostPreparationResponse struct{ inner preparation.Backend }

func (p lostPreparationResponse) PrepareOwned(ctx context.Context, r preparation.Request) (preparation.Artifact, error) {
	if _, err := p.inner.PrepareOwned(ctx, r); err != nil {
		return preparation.Artifact{}, err
	}
	return preparation.Artifact{}, preparation.ErrPreparationUnknown
}
func (p lostPreparationResponse) InspectOwned(ctx context.Context, r document.PublicationReadWorkRequest, a preparation.Artifact) (contracts.IndexSnapshot, error) {
	return p.inner.InspectOwned(ctx, r, a)
}

type echoPreparation struct {
	inner preparation.Backend
	work  document.PublicationWork
}

func (p echoPreparation) PrepareOwned(_ context.Context, r preparation.Request) (preparation.Artifact, error) {
	a, _, err := preparation.Build(p.work, r.Profile, r.Chunks)
	return a, err
}
func (p echoPreparation) InspectOwned(ctx context.Context, r document.PublicationReadWorkRequest, a preparation.Artifact) (contracts.IndexSnapshot, error) {
	return p.inner.InspectOwned(ctx, r, a)
}

type corruptSplitter struct{}

func (corruptSplitter) Split(ctx context.Context, r contracts.SplitRequest) ([]contracts.Chunk, error) {
	r.Blocks[0].Content.Text = "伪造正文"
	return (chunk.SimpleSplitter{}).Split(ctx, r)
}

func verifyPreparationIndex(ctx context.Context, db *sql.DB, scope contracts.AccessScope, request contracts.RequestContext, tasks postgres.PublicationTasks, newTask func(string) (publicationTaskFixture, error)) error {
	makeAccess := func(name string, duration time.Duration) (publicationTaskFixture, document.PublicationReadWorkRequest, error) {
		f, err := newTask(name)
		if err != nil {
			return f, document.PublicationReadWorkRequest{}, err
		}
		r := request
		r.ProfileDigest = f.Start.Job.ProfileDigest
		r.RequestID = contracts.ID(name + "-claim")
		s := scope
		s.SubjectID = "worker-a"
		s.ScopeID = "index-worker-scope"
		s.Action = "publication.prepare"
		claim, err := tasks.Claim(ctx, document.PublicationClaimRequest{CommandID: contracts.ID(name + "-claim"), ClaimRequest: contracts.ClaimRequest{Request: r, Scope: s, JobID: f.Start.Job.ID, WorkerID: "worker-a", LeaseDuration: duration}})
		if err != nil {
			return f, document.PublicationReadWorkRequest{}, fmt.Errorf("preparation fixture claim case=%s requested_lease=%s: %w", name, duration, err)
		}
		r.RequestID = contracts.ID(name + "-prepare")
		return f, document.PublicationReadWorkRequest{Request: r, Scope: s, Lease: claim.Lease}, err
	}
	resolve := func(_ context.Context, b document.PublicationBackendSnapshot) (contracts.IndexProfile, error) {
		p, err := referenceIndexProfile(b.IndexProfile.Version)
		if err != nil || p.ID != b.IndexProfile.ID || p.Digest != b.ProfileDigest {
			return contracts.IndexProfile{}, errors.New("unbound index profile")
		}
		return p, nil
	}
	backend := postgres.PreparationIndex{Tasks: tasks, ModuleInstanceID: "postgres-keyword-preparation"}
	worker := preparation.Worker{Tasks: tasks, Splitter: chunk.SimpleSplitter{}, SplitterInstanceID: "native-character-splitter", ResolveProfile: resolve, Backend: backend}
	countArtifacts := func(operation contracts.ID) (int, error) {
		var count int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.publication_prepared_artifacts WHERE operation_id=$1`, string(operation)).Scan(&count)
		return count, err
	}
	assertRolledBack := func(f publicationTaskFixture, access document.PublicationReadWorkRequest) error {
		set, generation := preparation.TargetIDs(f.Start.Operation.ID, access.Lease.AttemptID)
		var count int
		if db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM akos_withdraw_v1.generation_sets WHERE id=$1)+(SELECT count(*) FROM akos_withdraw_v1.index_generations WHERE id=$2)+(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$2)+(SELECT count(*) FROM akos_withdraw_v1.publication_prepared_artifacts WHERE operation_id=$3)`, string(set), string(generation), string(f.Start.Operation.ID)).Scan(&count) != nil || count != 0 {
			return errors.New("failed prepare retained partial index")
		}
		return nil
	}
	f, access, err := makeAccess("actual-index", 8*time.Second)
	if err != nil {
		return err
	}
	worker.Backend = lostPreparationResponse{backend}
	if a, err := worker.Prepare(ctx, access); !errors.Is(err, preparation.ErrPreparationUnknown) || a.OperationID != "" {
		return errors.New("actual index response not lost")
	}
	var payload []byte
	var committed preparation.Artifact
	if db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_prepared_artifacts WHERE operation_id=$1`, string(f.Start.Operation.ID)).Scan(&payload) != nil || json.Unmarshal(payload, &committed) != nil {
		return errors.New("actual prepared index not durable")
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	coldTasks := tasks
	coldTasks.DB = cold
	coldBackend := postgres.PreparationIndex{Tasks: coldTasks, ModuleInstanceID: backend.ModuleInstanceID}
	worker.Tasks = coldTasks
	worker.Backend = coldBackend
	access.Request.RequestID = "actual-index-cold-retry"
	a, err := worker.Prepare(ctx, access)
	if err != nil || !reflect.DeepEqual(a, committed) {
		return errors.New("cold preparation replay changed output")
	}
	snapshot, err := coldBackend.InspectOwned(ctx, access, a)
	if err != nil || len(snapshot.Records) != 2 {
		return errors.New("actual keyword readback count/source differs")
	}
	if err := recordPreparationSample(a, snapshot); err != nil {
		return err
	}
	for _, r := range snapshot.Records {
		if r.Published || r.Citation.DocumentID != f.Approved.Revision.DocumentID || r.Citation.RevisionID != f.Approved.Revision.ID || r.Chunk.ProfileDigest != a.Profile.Digest {
			return errors.New("candidate visibility or citation scope lost")
		}
	}
	for _, table := range []string{"publication_prepared_artifacts", "publication_preparation_audit", "publication_preparation_outbox"} {
		var count int
		if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE operation_id=$1`, string(f.Start.Operation.ID)).Scan(&count) != nil || count != 1 {
			return errors.New("preparation retry duplicated effects")
		}
	}
	generation := a.GenerationSet.Channels[contracts.KeywordChannel].ID
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.index_projection_rows SET payload=jsonb_set(payload,'{Chunk,Text}','"changed"') WHERE generation_id=$1`, string(generation)); err == nil {
		return errors.New("sealed preparation mutable")
	}
	// Existing deletion control is allowed to fence/remove the candidate. An old
	// ready proof can no longer pass independent live-backend verification.
	deleteScope := access.Scope
	deleteScope.Action = "index.delete"
	cleanup := postgres.IndexCleanup{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.Action != "index.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("fixture candidate cleanup denied")
		}
		return nil
	}}
	if err := cleanup.Delete(ctx, contracts.IndexDeleteRequest{Request: access.Request, Scope: deleteScope, GenerationID: generation}); err != nil {
		return err
	}
	if _, err := coldBackend.InspectOwned(ctx, access, a); err == nil {
		return errors.New("deleted index accepted echoed proof")
	}
	f, access, err = makeAccess("echo-index", 5*time.Second)
	if err != nil {
		return err
	}
	work, err := tasks.ReadOwned(ctx, access)
	if err != nil {
		return err
	}
	worker.Tasks = tasks
	worker.Backend = echoPreparation{backend, work}
	if a, err := worker.Prepare(ctx, access); err == nil || a.OperationID != "" {
		return errors.New("echo-only index preparation accepted")
	}
	if count, err := countArtifacts(f.Start.Operation.ID); err != nil || count != 0 {
		return errors.New("echo-only adapter wrote an artifact")
	}
	f, access, err = makeAccess("corrupt-splitter", 5*time.Second)
	if err != nil {
		return err
	}
	worker.Backend = backend
	worker.Splitter = corruptSplitter{}
	if a, err := worker.Prepare(ctx, access); err == nil || a.OperationID != "" {
		return errors.New("splitter altered approved source")
	}
	if err := assertRolledBack(f, access); err != nil {
		return err
	}
	worker.Splitter = chunk.SimpleSplitter{}
	for _, table := range []string{"publication_preparation_audit", "publication_preparation_outbox"} {
		f, access, err := makeAccess("fail-"+table, 5*time.Second)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_preparation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture preparation failure'; END $$`); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER preparation_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_preparation()`); err != nil {
			return err
		}
		_, prepareErr := worker.Prepare(ctx, access)
		_, dropErr := db.ExecContext(ctx, `DROP TRIGGER preparation_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if prepareErr == nil {
			return errors.New("partial preparation succeeded")
		}
		if err := assertRolledBack(f, access); err != nil {
			return err
		}
	}
	f, access, err = makeAccess("missing-preparation-event", 5*time.Second)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_preparation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER preparation_skip BEFORE INSERT ON akos_withdraw_v1.publication_preparation_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_preparation()`); err != nil {
		return err
	}
	_, prepareErr := worker.Prepare(ctx, access)
	_, dropErr := db.ExecContext(ctx, `DROP TRIGGER preparation_skip ON akos_withdraw_v1.publication_preparation_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if !errors.Is(prepareErr, preparation.ErrPreparationUnknown) {
		return errors.New("missing preparation event passed deferred gate")
	}
	if err := assertRolledBack(f, access); err != nil {
		return err
	}
	if _, err := worker.Prepare(ctx, access); err != nil {
		return errors.New("same attempt did not recover confirmed rollback")
	}
	f, access, err = makeAccess("revoked-index-write", 5*time.Second)
	if err != nil {
		return err
	}
	revoking := tasks
	calls := 0
	revoking.AuthorizeTask = func(c context.Context, a document.PublicationTaskAccess) error {
		if a.Kind == "prepare_index" {
			calls++
			if calls == 3 {
				return errors.New("index writer revoked at commit")
			}
		}
		return tasks.AuthorizeTask(c, a)
	}
	worker.Backend = postgres.PreparationIndex{Tasks: revoking, ModuleInstanceID: backend.ModuleInstanceID}
	if _, err := worker.Prepare(ctx, access); err == nil || calls != 3 {
		return errors.New("precommit writer revocation accepted")
	}
	if err := assertRolledBack(f, access); err != nil {
		return err
	}
	worker.Backend = backend
	f, access, err = makeAccess("index-takeover", 500*time.Millisecond)
	if err != nil {
		return err
	}
	old, err := worker.Prepare(ctx, access)
	if err != nil {
		return errors.New("old index attempt failed to prepare")
	}
	if _, err := db.ExecContext(ctx, `SELECT pg_sleep(0.55)`); err != nil {
		return err
	}
	r := access.Request
	r.RequestID = "index-takeover-claim"
	s := access.Scope
	s.SubjectID = "worker-b"
	lease, err := tasks.Claim(ctx, document.PublicationClaimRequest{CommandID: "index-takeover-claim", ClaimRequest: contracts.ClaimRequest{Request: r, Scope: s, JobID: f.Start.Job.ID, WorkerID: "worker-b", LeaseDuration: 5 * time.Second}})
	if err != nil {
		return err
	}
	if _, err := worker.Prepare(ctx, access); err == nil {
		return errors.New("old worker wrote after takeover")
	}
	access.Scope = s
	access.Lease = lease.Lease
	access.Request.RequestID = "index-takeover-prepare"
	next, err := worker.Prepare(ctx, access)
	if err != nil || next.GenerationSet.ID == old.GenerationSet.ID || next.FencingToken != old.FencingToken+1 {
		return errors.New("new attempt did not isolate index generations")
	}
	if _, err := backend.InspectOwned(ctx, access, old); err == nil {
		return errors.New("new lease accepted old attempt proof")
	}
	if count, err := countArtifacts(f.Start.Operation.ID); err != nil || count != 2 {
		return errors.New("attempt namespace outputs overwritten")
	}
	snapshot, err = backend.InspectOwned(ctx, access, next)
	if err != nil {
		return err
	}
	if err := recordPreparationSample(next, snapshot); err != nil {
		return err
	}
	f, access, err = makeAccess("index-write-fence", 5*time.Second)
	if err != nil {
		return err
	}
	fenced := tasks
	checks := 0
	fenced.AuthorizeTask = func(c context.Context, a document.PublicationTaskAccess) error {
		if a.Kind == "prepare_index" {
			checks++
			if checks == 3 {
				tx, err := cold.BeginTx(c, nil)
				if err != nil {
					return err
				}
				var id string
				err = tx.QueryRowContext(c, `SELECT id FROM akos_withdraw_v1.publication_task_heads WHERE id=$1 FOR UPDATE NOWAIT`, string(a.JobID)).Scan(&id)
				tx.Rollback()
				var code interface{ SQLState() string }
				if !errors.As(err, &code) || code.SQLState() != "55P03" {
					return errors.New("index write did not fence lease mutation")
				}
			}
		}
		return tasks.AuthorizeTask(c, a)
	}
	worker.Backend = postgres.PreparationIndex{Tasks: fenced, ModuleInstanceID: backend.ModuleInstanceID}
	if _, err := worker.Prepare(ctx, access); err != nil || checks != 3 {
		return errors.New("atomic index/lease fence check failed")
	}
	// Same-attempt concurrent preparation is immutable and idempotent.
	f, access, err = makeAccess("concurrent-index", 5*time.Second)
	if err != nil {
		return err
	}
	worker.Backend = backend
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := worker.Prepare(ctx, access); results <- err == nil }()
	}
	wg.Wait()
	close(results)
	success := 0
	for ok := range results {
		if ok {
			success++
		}
	}
	if success < 1 {
		return errors.New("concurrent prepare had no winner")
	}
	if _, err := worker.Prepare(ctx, access); err != nil {
		return errors.New("concurrent prepare could not reconcile")
	}
	if count, err := countArtifacts(f.Start.Operation.ID); err != nil || count != 1 {
		return errors.New("concurrent prepare duplicated manifest")
	}
	var inactive bool
	if db.QueryRowContext(ctx, `SELECT publication_epoch=0 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Approved.Revision.DocumentID)).Scan(&inactive) != nil || !inactive {
		return errors.New("index preparation published content")
	}
	return verifyPublicationCompletion(ctx, db, tasks, makeAccess, worker, backend)
}

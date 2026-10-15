-- PostgreSQL 18 verification prototype; not a production migration/bootstrap.
BEGIN;
CREATE SCHEMA akos_withdraw_v1;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;
CREATE DOMAIN platform_id AS text CHECK(char_length(VALUE) BETWEEN 1 AND 200);
-- Unconstrained numeric + integer check avoids numeric(20,0) rounding input.
CREATE DOMAIN uint64_value AS numeric CHECK(VALUE >= 0 AND VALUE <= 18446744073709551615 AND VALUE=trunc(VALUE));
CREATE DOMAIN sha256_value AS text CHECK(VALUE ~ '^[0-9a-f]{64}$');
-- Preserve nanoseconds as normalized UTC text; adapters must emit nine digits.
CREATE DOMAIN utc_nanos AS text CHECK(VALUE ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T(0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\.[0-9]{9}Z$' AND left(VALUE,4)<>'0000' AND VALUE::timestamptz IS NOT NULL);
CREATE TABLE documents(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,id platform_id NOT NULL,
 publication_epoch uint64_value NOT NULL,row_version uint64_value NOT NULL CHECK(row_version>0),
 active_revision_id platform_id,active_generation_set_id platform_id,deleted boolean NOT NULL DEFAULT false,
 PRIMARY KEY(space_id,kb_id,id),UNIQUE(id),
 CHECK((active_revision_id IS NULL)=(active_generation_set_id IS NULL))
);
CREATE TABLE revisions(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,id platform_id NOT NULL,
 content_digest sha256_value NOT NULL,
 PRIMARY KEY(space_id,kb_id,document_id,id),UNIQUE(id),
 FOREIGN KEY(space_id,kb_id,document_id) REFERENCES documents(space_id,kb_id,id)
);
CREATE TABLE generation_sets(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,revision_id platform_id NOT NULL,id platform_id NOT NULL,
 backend_epoch_id platform_id NOT NULL,
 PRIMARY KEY(space_id,kb_id,document_id,revision_id,id),UNIQUE(id),UNIQUE(space_id,kb_id,document_id,revision_id,id,backend_epoch_id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id) REFERENCES revisions(space_id,kb_id,document_id,id)
);
ALTER TABLE documents ADD FOREIGN KEY(space_id,kb_id,id,active_revision_id) REFERENCES revisions(space_id,kb_id,document_id,id);
ALTER TABLE documents ADD FOREIGN KEY(space_id,kb_id,id,active_revision_id,active_generation_set_id) REFERENCES generation_sets(space_id,kb_id,document_id,revision_id,id);
CREATE TABLE index_generations(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,revision_id platform_id NOT NULL,
 generation_set_id platform_id NOT NULL,id platform_id NOT NULL,backend_epoch_id platform_id NOT NULL,
 channel text NOT NULL CHECK(channel IN('keyword','vector','graph')),profile_digest sha256_value NOT NULL,module_instance_id platform_id NOT NULL,
 PRIMARY KEY(space_id,kb_id,document_id,revision_id,generation_set_id,id),UNIQUE(id),UNIQUE(generation_set_id,channel),
 UNIQUE(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,generation_set_id,backend_epoch_id)
  REFERENCES generation_sets(space_id,kb_id,document_id,revision_id,id,backend_epoch_id)
);
CREATE TABLE command_operations(
 id platform_id PRIMARY KEY,space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,
 actor_semantic_digest sha256_value NOT NULL,actor_id platform_id NOT NULL,action text NOT NULL CHECK(action IN('publish','withdraw','delete')),
 idempotency_key platform_id NOT NULL,command_digest sha256_value NOT NULL,
 state text NOT NULL CHECK(state IN('pending','committed')),history_id platform_id,
 UNIQUE(space_id,actor_semantic_digest,action,idempotency_key),
 UNIQUE(space_id,kb_id,document_id,id,command_digest,actor_id,action),UNIQUE(space_id,kb_id,document_id,id,state),
 CHECK((state='pending' AND history_id IS NULL) OR (state='committed' AND history_id IS NOT NULL)),
 FOREIGN KEY(space_id,kb_id,document_id) REFERENCES documents(space_id,kb_id,id)
);
CREATE TABLE publication_history(
 id platform_id PRIMARY KEY,operation_id platform_id NOT NULL UNIQUE,
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,
 previous_epoch uint64_value NOT NULL,publication_epoch uint64_value NOT NULL,
 action text NOT NULL CHECK(action IN('publish','withdraw','delete')),
 previous_revision_id platform_id,previous_generation_set_id platform_id,revision_id platform_id,generation_set_id platform_id,
 operation_state text NOT NULL DEFAULT 'committed' CHECK(operation_state='committed'),
 actor_id platform_id NOT NULL,actor jsonb NOT NULL,reason text NOT NULL CHECK(char_length(reason)<=10000),
 occurred_at utc_nanos NOT NULL,command_digest sha256_value NOT NULL,
 CHECK(previous_epoch<18446744073709551615 AND publication_epoch=previous_epoch+1),
 CHECK((previous_revision_id IS NULL)=(previous_generation_set_id IS NULL)),
 CHECK((revision_id IS NULL)=(generation_set_id IS NULL)),
 CHECK(previous_epoch>0 OR previous_revision_id IS NULL),
 CHECK((action='publish' AND revision_id IS NOT NULL) OR
       (action='withdraw' AND previous_epoch>0 AND previous_revision_id IS NOT NULL AND revision_id IS NULL AND reason<>'') OR
       (action='delete' AND revision_id IS NULL)),
 CHECK(jsonb_typeof(actor)='object' AND jsonb_typeof(actor->'subject_id')='string' AND jsonb_typeof(actor->'subject_type')='string' AND jsonb_typeof(actor->'issuer')='string' AND coalesce(actor->>'subject_id'=actor_id,false) AND
       coalesce(actor->>'subject_type' IN('user','service'),false) AND coalesce(length(actor->>'issuer')>0,false)),
 UNIQUE(space_id,kb_id,document_id,publication_epoch),
 UNIQUE(space_id,kb_id,document_id,id,operation_id),UNIQUE(space_id,kb_id,document_id,id,operation_id,publication_epoch),
 UNIQUE(space_id,kb_id,document_id,id,operation_id,previous_epoch,previous_revision_id,previous_generation_set_id,action),
 FOREIGN KEY(space_id,kb_id,document_id,operation_id,command_digest,actor_id,action)
  REFERENCES command_operations(space_id,kb_id,document_id,id,command_digest,actor_id,action),
 FOREIGN KEY(space_id,kb_id,document_id,operation_id,operation_state)
  REFERENCES command_operations(space_id,kb_id,document_id,id,state) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(space_id,kb_id,document_id,previous_revision_id,previous_generation_set_id)
  REFERENCES generation_sets(space_id,kb_id,document_id,revision_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,generation_set_id)
  REFERENCES generation_sets(space_id,kb_id,document_id,revision_id,id)
);
ALTER TABLE command_operations ADD FOREIGN KEY(space_id,kb_id,document_id,history_id,id)
 REFERENCES publication_history(space_id,kb_id,document_id,id,operation_id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE cleanup_plans(
 id platform_id PRIMARY KEY,history_id platform_id NOT NULL UNIQUE,operation_id platform_id NOT NULL,
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,
 revision_id platform_id NOT NULL,generation_set_id platform_id NOT NULL,publication_epoch uint64_value NOT NULL,
 history_action text NOT NULL DEFAULT 'withdraw' CHECK(history_action IN('withdraw','publish')),
 profile_digest sha256_value NOT NULL,created_at utc_nanos NOT NULL,
 UNIQUE(space_id,kb_id,document_id,id,history_id,operation_id),UNIQUE(space_id,kb_id,id),UNIQUE(space_id,kb_id,document_id,revision_id,generation_set_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,history_id,operation_id,publication_epoch,revision_id,generation_set_id,history_action)
  REFERENCES publication_history(space_id,kb_id,document_id,id,operation_id,previous_epoch,previous_revision_id,previous_generation_set_id,action)
);
CREATE TABLE binding_definitions(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,id platform_id NOT NULL,module_instance_id platform_id NOT NULL,
 profile_digest sha256_value NOT NULL,kind text NOT NULL CHECK(kind IN('index','graph','memory','cache')),
 definition jsonb NOT NULL CHECK(jsonb_typeof(definition)='object'),
 PRIMARY KEY(space_id,kb_id,id),UNIQUE(space_id,kb_id,id,module_instance_id,profile_digest,kind)
);
-- Registered non-index dependencies of a specific published source epoch.
CREATE TABLE publication_cleanup_dependencies(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,
 revision_id platform_id NOT NULL,generation_set_id platform_id NOT NULL,publication_epoch uint64_value NOT NULL CHECK(publication_epoch>0),
 binding_reference platform_id NOT NULL,module_instance_id platform_id NOT NULL,profile_digest sha256_value NOT NULL,
 kind text NOT NULL CHECK(kind IN('graph','memory','cache')),
 PRIMARY KEY(space_id,kb_id,document_id,publication_epoch,binding_reference),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,generation_set_id)
 REFERENCES generation_sets(space_id,kb_id,document_id,revision_id,id),
 FOREIGN KEY(space_id,kb_id,binding_reference,module_instance_id,profile_digest,kind)
 REFERENCES binding_definitions(space_id,kb_id,id,module_instance_id,profile_digest,kind)
);
CREATE TABLE cleanup_targets(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,revision_id platform_id NOT NULL,generation_set_id platform_id NOT NULL,
 plan_id platform_id NOT NULL,id platform_id NOT NULL,kind text NOT NULL CHECK(kind IN('index','graph','memory','cache')),
 module_instance_id platform_id NOT NULL,binding_reference platform_id NOT NULL,profile_digest sha256_value NOT NULL,
 generation_id platform_id,backend_epoch_id platform_id,channel text,
 PRIMARY KEY(plan_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,generation_set_id,plan_id)
  REFERENCES cleanup_plans(space_id,kb_id,document_id,revision_id,generation_set_id,id),
 FOREIGN KEY(space_id,kb_id,binding_reference,module_instance_id,profile_digest,kind)
  REFERENCES binding_definitions(space_id,kb_id,id,module_instance_id,profile_digest,kind),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,generation_set_id,generation_id,backend_epoch_id,channel,profile_digest,module_instance_id)
  REFERENCES index_generations(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id),
 CHECK((kind='index' AND generation_id IS NOT NULL AND backend_epoch_id IS NOT NULL AND channel IS NOT NULL AND channel IN('keyword','vector','graph')) OR
       (kind<>'index' AND generation_id IS NULL AND backend_epoch_id IS NULL AND channel IS NULL))
);
CREATE UNIQUE INDEX cleanup_index_unique ON cleanup_targets(plan_id,generation_id) WHERE kind='index';
CREATE TABLE cleanup_progress(
 plan_id platform_id NOT NULL,target_id platform_id NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','running','succeeded','failed','unknown')),
 fencing_token uint64_value NOT NULL DEFAULT 0,attempt uint64_value NOT NULL DEFAULT 0,
 row_version uint64_value NOT NULL DEFAULT 1 CHECK(row_version>0),
 worker_id platform_id,lease_until timestamptz,result_reference platform_id,
 PRIMARY KEY(plan_id,target_id),FOREIGN KEY(plan_id,target_id) REFERENCES cleanup_targets(plan_id,id),
 CHECK((state='pending' AND worker_id IS NULL AND lease_until IS NULL AND fencing_token=0 AND attempt=0)
  OR (state='running' AND worker_id IS NOT NULL AND lease_until IS NOT NULL AND fencing_token>0 AND attempt>0)
  OR (state IN('succeeded','failed','unknown') AND worker_id IS NOT NULL AND lease_until IS NULL AND fencing_token>0 AND attempt>0)),
 CHECK((state='succeeded' AND result_reference IS NOT NULL) OR (state<>'succeeded' AND result_reference IS NULL))
);
CREATE FUNCTION initialize_cleanup_progress() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO akos_withdraw_v1.cleanup_progress(plan_id,target_id) VALUES(NEW.plan_id,NEW.id);
 RETURN NEW;
END; $$;
CREATE TRIGGER target_progress AFTER INSERT ON cleanup_targets
 FOR EACH ROW EXECUTE FUNCTION initialize_cleanup_progress();
CREATE TABLE withdraw_outbox(
 id platform_id PRIMARY KEY,space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,
 plan_id platform_id NOT NULL,history_id platform_id NOT NULL,operation_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type='publication.withdrawn'),aggregate_version uint64_value NOT NULL,
 FOREIGN KEY(space_id,kb_id,document_id,plan_id,history_id,operation_id)
  REFERENCES cleanup_plans(space_id,kb_id,document_id,id,history_id,operation_id),
 FOREIGN KEY(space_id,kb_id,document_id,history_id,operation_id,aggregate_version)
  REFERENCES publication_history(space_id,kb_id,document_id,id,operation_id,publication_epoch),
 UNIQUE(plan_id,event_type)
);
-- Deferred coverage checks see the complete transaction, not partial inserts.
CREATE TABLE withdraw_audit(
 id platform_id PRIMARY KEY,request_id platform_id NOT NULL,
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,
 history_id platform_id NOT NULL UNIQUE,operation_id platform_id NOT NULL UNIQUE,
 action text NOT NULL CHECK(action='publication.withdraw'),
 actor_id platform_id NOT NULL,actor jsonb NOT NULL,occurred_at utc_nanos NOT NULL,
 FOREIGN KEY(space_id,kb_id,document_id,history_id,operation_id)
 REFERENCES publication_history(space_id,kb_id,document_id,id,operation_id)
);
CREATE FUNCTION validate_withdraw_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_history h
  WHERE h.id=NEW.history_id AND h.operation_id=NEW.operation_id
  AND h.space_id=NEW.space_id AND h.kb_id=NEW.kb_id AND h.document_id=NEW.document_id
  AND h.action='withdraw' AND h.actor_id=NEW.actor_id AND h.actor=NEW.actor AND h.occurred_at=NEW.occurred_at)
 THEN RAISE EXCEPTION 'audit does not match withdrawal history' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER audit_history_match BEFORE INSERT ON withdraw_audit
 FOR EACH ROW EXECUTE FUNCTION validate_withdraw_audit();
CREATE FUNCTION register_current_dependency() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_head akos_withdraw_v1.documents%ROWTYPE;
BEGIN
 SELECT * INTO current_head FROM akos_withdraw_v1.documents
 WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND id=NEW.document_id FOR UPDATE;
 IF NOT FOUND OR current_head.deleted OR current_head.publication_epoch<>NEW.publication_epoch
 OR current_head.active_revision_id IS DISTINCT FROM NEW.revision_id
 OR current_head.active_generation_set_id IS DISTINCT FROM NEW.generation_set_id
 THEN RAISE EXCEPTION 'dependency source is no longer published' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER dependency_current BEFORE INSERT ON publication_cleanup_dependencies
 FOR EACH ROW EXECUTE FUNCTION register_current_dependency();
CREATE FUNCTION require_open_cleanup_plan() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE operation_state text;
BEGIN
 SELECT o.state INTO operation_state FROM akos_withdraw_v1.cleanup_plans p
 JOIN akos_withdraw_v1.command_operations o ON o.id=p.operation_id
 WHERE p.id=NEW.plan_id AND p.space_id=NEW.space_id AND p.kb_id=NEW.kb_id
 AND p.document_id=NEW.document_id FOR UPDATE OF o;
 IF FOUND AND operation_state<>'pending'
 THEN RAISE EXCEPTION 'cleanup plan is sealed' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER target_open_plan BEFORE INSERT ON cleanup_targets
 FOR EACH ROW EXECUTE FUNCTION require_open_cleanup_plan();
CREATE FUNCTION require_cleanup_targets() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected integer; actual integer; history_time text;
BEGIN
 SELECT count(*) INTO expected FROM akos_withdraw_v1.index_generations
  WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND document_id=NEW.document_id
  AND revision_id=NEW.revision_id AND generation_set_id=NEW.generation_set_id;
 SELECT count(*) INTO actual FROM akos_withdraw_v1.cleanup_targets WHERE plan_id=NEW.id AND kind='index';
 IF expected<1 OR expected>3 OR actual<>expected THEN
  RAISE EXCEPTION 'incomplete cleanup index coverage' USING ERRCODE='23514';
 END IF;
 IF EXISTS(
  SELECT 1 FROM akos_withdraw_v1.publication_cleanup_dependencies d
  WHERE d.space_id=NEW.space_id AND d.kb_id=NEW.kb_id AND d.document_id=NEW.document_id
  AND d.revision_id=NEW.revision_id AND d.generation_set_id=NEW.generation_set_id AND d.publication_epoch=NEW.publication_epoch
  AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_targets t WHERE t.plan_id=NEW.id
   AND t.kind=d.kind AND t.binding_reference=d.binding_reference AND t.module_instance_id=d.module_instance_id AND t.profile_digest=d.profile_digest)
 ) OR EXISTS(
  SELECT 1 FROM akos_withdraw_v1.cleanup_targets t WHERE t.plan_id=NEW.id AND t.kind<>'index'
  AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_cleanup_dependencies d
   WHERE d.space_id=NEW.space_id AND d.kb_id=NEW.kb_id AND d.document_id=NEW.document_id
   AND d.revision_id=NEW.revision_id AND d.generation_set_id=NEW.generation_set_id AND d.publication_epoch=NEW.publication_epoch
   AND d.kind=t.kind AND d.binding_reference=t.binding_reference AND d.module_instance_id=t.module_instance_id AND d.profile_digest=t.profile_digest)
 ) THEN RAISE EXCEPTION 'cleanup source dependency coverage differs' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_targets t JOIN akos_withdraw_v1.binding_definitions b
  ON b.space_id=t.space_id AND b.kb_id=t.kb_id AND b.id=t.binding_reference
  WHERE t.plan_id=NEW.id AND t.kind='index' AND b.definition->>'generation_id' IS DISTINCT FROM t.generation_id::text)
 THEN RAISE EXCEPTION 'index binding differs from generation' USING ERRCODE='23514'; END IF;
 SELECT occurred_at INTO history_time FROM akos_withdraw_v1.publication_history WHERE id=NEW.history_id;
 IF (NEW.created_at::text COLLATE "C") < (history_time COLLATE "C") THEN
  RAISE EXCEPTION 'cleanup plan predates history' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER cleanup_coverage AFTER INSERT ON cleanup_plans
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_cleanup_targets();
CREATE FUNCTION require_withdraw_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.action='withdraw' AND NOT EXISTS(
  SELECT 1 FROM akos_withdraw_v1.cleanup_plans p JOIN akos_withdraw_v1.withdraw_outbox e ON e.plan_id=p.id
  WHERE p.history_id=NEW.id AND e.history_id=NEW.id AND e.aggregate_version=NEW.publication_epoch
 ) THEN RAISE EXCEPTION 'withdraw lacks cleanup plan/outbox' USING ERRCODE='23514'; END IF;
 IF NEW.action='withdraw' AND NOT EXISTS(
  SELECT 1 FROM akos_withdraw_v1.withdraw_audit a WHERE a.history_id=NEW.id AND a.operation_id=NEW.operation_id
 ) THEN RAISE EXCEPTION 'withdraw lacks audit' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER withdraw_delivery AFTER INSERT ON publication_history
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_withdraw_outbox();
-- Restrict application DML too; production roles/grants are still to be designed.
CREATE TABLE index_projection_rows(
 generation_id platform_id NOT NULL,chunk_id platform_id NOT NULL,payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(generation_id,chunk_id),FOREIGN KEY(generation_id) REFERENCES index_generations(id)
);
CREATE TABLE index_deletion_fences(
 generation_id platform_id PRIMARY KEY,deleted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(generation_id) REFERENCES index_generations(id)
);
ALTER TABLE index_generations ADD COLUMN projection_epoch uint64_value NOT NULL DEFAULT 0;
CREATE TABLE index_chunk_deletion_fences(
 generation_id platform_id NOT NULL,chunk_id platform_id NOT NULL,
 PRIMARY KEY(generation_id,chunk_id),FOREIGN KEY(generation_id) REFERENCES index_generations(id)
);
CREATE FUNCTION advance_index_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM akos_withdraw_v1.index_generations WHERE id=NEW.generation_id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences WHERE generation_id=NEW.generation_id) THEN
  UPDATE akos_withdraw_v1.index_generations SET projection_epoch=projection_epoch+1 WHERE id=NEW.generation_id AND projection_epoch<18446744073709551615;
  IF NOT FOUND THEN RAISE EXCEPTION 'projection fence unavailable' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER index_fence_epoch BEFORE INSERT ON index_deletion_fences FOR EACH ROW EXECUTE FUNCTION advance_index_fence();
CREATE TRIGGER chunk_fence_epoch BEFORE INSERT ON index_chunk_deletion_fences FOR EACH ROW EXECUTE FUNCTION advance_index_fence();
CREATE FUNCTION reject_deleted_projection() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM akos_withdraw_v1.index_generations WHERE id=NEW.generation_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences WHERE generation_id=NEW.generation_id)
 OR EXISTS(SELECT 1 FROM akos_withdraw_v1.index_chunk_deletion_fences WHERE generation_id=NEW.generation_id AND chunk_id=NEW.chunk_id)
 THEN RAISE EXCEPTION 'generation is deleted' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER projection_fence BEFORE INSERT OR UPDATE ON index_projection_rows
 FOR EACH ROW EXECUTE FUNCTION reject_deleted_projection();
CREATE FUNCTION immutable_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable authoritative record' USING ERRCODE='23514'; END; $$;
CREATE TRIGGER binding_immutable BEFORE UPDATE OR DELETE ON binding_definitions FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER history_immutable BEFORE UPDATE OR DELETE ON publication_history FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER plan_immutable BEFORE UPDATE OR DELETE ON cleanup_plans FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER target_immutable BEFORE UPDATE OR DELETE ON cleanup_targets FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER audit_immutable BEFORE UPDATE OR DELETE ON withdraw_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER dependency_immutable BEFORE UPDATE OR DELETE ON publication_cleanup_dependencies FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER index_fence_immutable BEFORE UPDATE OR DELETE ON index_deletion_fences FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER chunk_fence_immutable BEFORE UPDATE OR DELETE ON index_chunk_deletion_fences FOR EACH ROW EXECUTE FUNCTION immutable_record();
-- Narrow candidate persistence slice. Full metadata UoW/bootstrap remains absent.
CREATE TABLE draft_heads(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,id platform_id NOT NULL,
 row_version uint64_value NOT NULL CHECK(row_version>0),state text NOT NULL CHECK(state IN('draft','submitted')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=8388608),
 PRIMARY KEY(space_id,kb_id,document_id,id),UNIQUE(id),
 FOREIGN KEY(space_id,kb_id,document_id) REFERENCES documents(space_id,kb_id,id),
 CHECK(coalesce(payload->>'ID'=id AND payload->>'DocumentID'=document_id AND payload->>'State'=state AND (payload->>'RowVersion')::numeric=row_version,false))
);
CREATE TABLE draft_candidates(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,draft_id platform_id NOT NULL,id platform_id NOT NULL,
 actor_id platform_id NOT NULL,request_digest sha256_value NOT NULL,payload jsonb NOT NULL,
 receipt jsonb NOT NULL,PRIMARY KEY(space_id,kb_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,draft_id) REFERENCES draft_heads(space_id,kb_id,document_id,id),
 CHECK(jsonb_typeof(payload)='object' AND jsonb_typeof(receipt)='object' AND octet_length(payload::text)<=8388608)
);
CREATE TABLE candidate_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,candidate_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,request_id platform_id NOT NULL,action text NOT NULL CHECK(action='document.parse'),
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,candidate_id),FOREIGN KEY(space_id,kb_id,candidate_id) REFERENCES draft_candidates(space_id,kb_id,id)
);
CREATE TABLE candidate_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,candidate_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type='draft.candidate.recorded'),payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(space_id,kb_id,candidate_id),FOREIGN KEY(space_id,kb_id,candidate_id) REFERENCES draft_candidates(space_id,kb_id,id)
);
CREATE TRIGGER candidate_immutable BEFORE UPDATE OR DELETE ON draft_candidates FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER candidate_audit_immutable BEFORE UPDATE OR DELETE ON candidate_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION require_candidate_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.candidate_audit WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND candidate_id=NEW.id AND actor_id=NEW.actor_id)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.candidate_outbox WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND candidate_id=NEW.id AND payload=NEW.receipt)
 THEN RAISE EXCEPTION 'candidate requires atomic audit and outbox' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER candidate_effects AFTER INSERT ON draft_candidates DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_candidate_effects();
-- Synchronous submission slice. Frozen bytes remain bytea, never jsonb-reencoded.
CREATE TABLE revision_contents(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,revision_id platform_id NOT NULL,
 hash_format text NOT NULL CHECK(hash_format='akos.freeze/1'),blocks_hash sha256_value NOT NULL,content_hash sha256_value NOT NULL,
 frozen_payload bytea NOT NULL CHECK(octet_length(frozen_payload) BETWEEN 1 AND 4194304),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=8388608),
 PRIMARY KEY(space_id,kb_id,document_id,revision_id),UNIQUE(revision_id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id) REFERENCES revisions(space_id,kb_id,document_id,id),
 CHECK(coalesce(payload->>'ID'=revision_id AND payload->>'DocumentID'=document_id AND payload->>'HashFormat'=hash_format AND payload->>'ContentHash'=content_hash AND payload->>'BlocksHash'=blocks_hash,false))
);
CREATE TABLE approval_requests(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,id platform_id NOT NULL,revision_id platform_id NOT NULL,
 row_version uint64_value NOT NULL,state text NOT NULL CHECK(state IN('submitted','approved','rejected','revoked')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(space_id,kb_id,document_id,id),UNIQUE(space_id,kb_id,id),UNIQUE(space_id,kb_id,document_id,revision_id,id),UNIQUE(id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id) REFERENCES revision_contents(space_id,kb_id,document_id,revision_id),
 CHECK(coalesce(payload->>'ID'=id AND payload->>'RevisionID'=revision_id AND payload->>'DocumentID'=document_id AND payload->>'State'=state AND (payload->>'RowVersion')::numeric=row_version,false))
);
CREATE TABLE submission_receipts(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,actor_id platform_id NOT NULL,command_key platform_id NOT NULL,
 document_id platform_id NOT NULL,draft_id platform_id NOT NULL,revision_id platform_id NOT NULL,approval_id platform_id NOT NULL,
 profile_digest sha256_value NOT NULL,review_policy_version uint64_value NOT NULL CHECK(review_policy_version>0),
 request_digest sha256_value NOT NULL,receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 PRIMARY KEY(space_id,actor_id,command_key),UNIQUE(space_id,kb_id,document_id,draft_id),UNIQUE(space_id,kb_id,revision_id),
 FOREIGN KEY(space_id,kb_id,document_id,draft_id) REFERENCES draft_heads(space_id,kb_id,document_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,approval_id) REFERENCES approval_requests(space_id,kb_id,document_id,revision_id,id),
 CHECK(coalesce(receipt->'Revision'->>'ID'=revision_id AND receipt->'Revision'->>'DocumentID'=document_id AND receipt->>'ApprovalRequestID'=approval_id,false))
);
CREATE TABLE submission_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,revision_id platform_id NOT NULL,actor_id platform_id NOT NULL,request_id platform_id NOT NULL,
 action text NOT NULL DEFAULT 'document.submit' CHECK(action='document.submit'),occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,revision_id),FOREIGN KEY(space_id,kb_id,revision_id) REFERENCES submission_receipts(space_id,kb_id,revision_id)
);
CREATE TABLE submission_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,revision_id platform_id NOT NULL,
 event_type text NOT NULL DEFAULT 'revision.submitted' CHECK(event_type='revision.submitted'),payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(space_id,kb_id,revision_id),FOREIGN KEY(space_id,kb_id,revision_id) REFERENCES submission_receipts(space_id,kb_id,revision_id)
);
CREATE TRIGGER revision_identity_immutable BEFORE UPDATE OR DELETE ON revisions FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER revision_content_immutable BEFORE UPDATE OR DELETE ON revision_contents FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER submitted_draft_immutable BEFORE UPDATE OR DELETE ON draft_heads FOR EACH ROW WHEN(OLD.state='submitted') EXECUTE FUNCTION immutable_record();
CREATE TRIGGER submission_receipt_immutable BEFORE UPDATE OR DELETE ON submission_receipts FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER submission_audit_immutable BEFORE UPDATE OR DELETE ON submission_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION require_submission_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.submission_audit WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND revision_id=NEW.revision_id AND actor_id=NEW.actor_id)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.submission_outbox WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND revision_id=NEW.revision_id AND payload=NEW.receipt)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.draft_heads WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND document_id=NEW.document_id AND id=NEW.draft_id AND state='submitted')
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.revision_contents WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND document_id=NEW.document_id AND revision_id=NEW.revision_id AND payload=NEW.receipt->'Revision')
 THEN RAISE EXCEPTION 'submission requires atomic version, draft, audit and outbox' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER submission_effects AFTER INSERT ON submission_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_submission_effects();
CREATE FUNCTION require_revision_submission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.submission_receipts WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND document_id=NEW.document_id AND revision_id=NEW.revision_id)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.revisions WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND document_id=NEW.document_id AND id=NEW.revision_id AND content_digest=NEW.content_hash)
 THEN RAISE EXCEPTION 'frozen revision requires matching submission' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER revision_submission AFTER INSERT ON revision_contents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_revision_submission();
-- Review decisions are append-only; the request carries the current decision.
ALTER TABLE revision_contents ADD UNIQUE(space_id,kb_id,document_id,revision_id,content_hash);
ALTER TABLE approval_requests ADD UNIQUE(space_id,kb_id,document_id,revision_id);
ALTER TABLE approval_requests ADD COLUMN current_decision_id platform_id;
ALTER TABLE approval_requests ADD CHECK((state='submitted' AND row_version=0 AND current_decision_id IS NULL) OR (state<>'submitted' AND row_version>0 AND current_decision_id IS NOT NULL));
CREATE TABLE review_decisions(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,revision_id platform_id NOT NULL,approval_request_id platform_id NOT NULL,id platform_id NOT NULL,
 row_version uint64_value NOT NULL CHECK(row_version>0),content_hash sha256_value NOT NULL,payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=131072),
 PRIMARY KEY(space_id,kb_id,id),UNIQUE(id),UNIQUE(space_id,kb_id,approval_request_id,row_version),UNIQUE(space_id,kb_id,document_id,revision_id,approval_request_id,id,row_version),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,approval_request_id) REFERENCES approval_requests(space_id,kb_id,document_id,revision_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,content_hash) REFERENCES revision_contents(space_id,kb_id,document_id,revision_id,content_hash),
 CHECK(coalesce(payload->>'ID'=id AND payload->>'RevisionID'=revision_id AND payload->>'ContentHash'=content_hash AND (payload->>'RowVersion')::numeric=row_version AND payload->>'Decision' IN('approve','reject','revoke') AND (payload->>'PolicyVersion')::numeric>0,false))
);
ALTER TABLE approval_requests ADD FOREIGN KEY(space_id,kb_id,document_id,revision_id,id,current_decision_id,row_version) REFERENCES review_decisions(space_id,kb_id,document_id,revision_id,approval_request_id,id,row_version);
CREATE TABLE review_receipts(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,actor_id platform_id NOT NULL,command_key platform_id NOT NULL,decision_id platform_id NOT NULL,
 profile_digest sha256_value NOT NULL,review_policy_version uint64_value NOT NULL CHECK(review_policy_version>0),request_digest sha256_value NOT NULL,
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),PRIMARY KEY(space_id,actor_id,command_key),UNIQUE(space_id,kb_id,decision_id),
 FOREIGN KEY(space_id,kb_id,decision_id) REFERENCES review_decisions(space_id,kb_id,id),
 CHECK(coalesce(receipt->>'ID'=decision_id AND receipt->>'ActorID'=actor_id AND (receipt->>'PolicyVersion')::numeric=review_policy_version,false))
);
CREATE TABLE review_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,decision_id platform_id NOT NULL,actor_id platform_id NOT NULL,request_id platform_id NOT NULL,
 action text NOT NULL DEFAULT 'revision.approve' CHECK(action='revision.approve'),occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,decision_id),FOREIGN KEY(space_id,kb_id,decision_id) REFERENCES review_decisions(space_id,kb_id,id)
);
CREATE TABLE review_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,decision_id platform_id NOT NULL,event_type text NOT NULL CHECK(event_type IN('revision.approved','revision.rejected','revision.approval_revoked')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),PRIMARY KEY(space_id,kb_id,decision_id),
 FOREIGN KEY(space_id,kb_id,decision_id) REFERENCES review_decisions(space_id,kb_id,id)
);
CREATE TRIGGER review_decision_immutable BEFORE UPDATE OR DELETE ON review_decisions FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER review_receipt_immutable BEFORE UPDATE OR DELETE ON review_receipts FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER review_audit_immutable BEFORE UPDATE OR DELETE ON review_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION guard_review_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.row_version<>OLD.row_version+1 OR NEW.current_decision_id IS NULL OR NEW.current_decision_id IS NOT DISTINCT FROM OLD.current_decision_id
 OR NOT((OLD.state='submitted' AND NEW.state IN('approved','rejected')) OR (OLD.state='approved' AND NEW.state='revoked'))
 OR (NEW.space_id,NEW.kb_id,NEW.document_id,NEW.id,NEW.revision_id) IS DISTINCT FROM (OLD.space_id,OLD.kb_id,OLD.document_id,OLD.id,OLD.revision_id)
 OR NEW.payload-ARRAY['State','RowVersion','PolicyVersion'] IS DISTINCT FROM OLD.payload-ARRAY['State','RowVersion','PolicyVersion']
 THEN RAISE EXCEPTION 'invalid review transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER review_request_transition BEFORE UPDATE ON approval_requests FOR EACH ROW EXECUTE FUNCTION guard_review_transition();
CREATE FUNCTION require_review_effects() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected_state text; expected_event text;
BEGIN
 expected_state:=CASE NEW.payload->>'Decision' WHEN 'approve' THEN 'approved' WHEN 'reject' THEN 'rejected' ELSE 'revoked' END;
 expected_event:=CASE NEW.payload->>'Decision' WHEN 'approve' THEN 'revision.approved' WHEN 'reject' THEN 'revision.rejected' ELSE 'revision.approval_revoked' END;
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.approval_requests WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND document_id=NEW.document_id AND id=NEW.approval_request_id AND current_decision_id=NEW.id AND row_version=NEW.row_version AND state=expected_state AND (payload->>'PolicyVersion')::numeric=(NEW.payload->>'PolicyVersion')::numeric)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.review_receipts WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND decision_id=NEW.id AND receipt=NEW.payload)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.review_audit WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND decision_id=NEW.id AND actor_id=NEW.payload->>'ActorID')
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.review_outbox WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND decision_id=NEW.id AND event_type=expected_event AND payload=NEW.payload)
 THEN RAISE EXCEPTION 'review requires atomic head, receipt, audit and event' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER review_effects AFTER INSERT ON review_decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_review_effects();
-- Current review policy and index plan are independent authoritative heads.
CREATE TABLE review_policy_snapshots(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,version uint64_value NOT NULL CHECK(version>0),digest sha256_value NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=65536),
 PRIMARY KEY(space_id,kb_id,version),
 CHECK(coalesce((payload->>'Version')::numeric=version AND payload->>'Digest'=digest,false))
);
CREATE TABLE review_policy_heads(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,policy_version uint64_value NOT NULL,row_version uint64_value NOT NULL CHECK(row_version>0),
 PRIMARY KEY(space_id,kb_id),FOREIGN KEY(space_id,kb_id,policy_version) REFERENCES review_policy_snapshots(space_id,kb_id,version)
);
CREATE TABLE publication_backend_epochs(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,id platform_id NOT NULL,payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=65536),
 PRIMARY KEY(space_id,kb_id,id),CHECK(coalesce(payload->>'EpochID'=id,false))
);
CREATE TABLE publication_backend_heads(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,backend_epoch_id platform_id NOT NULL,row_version uint64_value NOT NULL CHECK(row_version>0),
 PRIMARY KEY(space_id,kb_id),FOREIGN KEY(space_id,kb_id,backend_epoch_id) REFERENCES publication_backend_epochs(space_id,kb_id,id)
);
CREATE TRIGGER review_policy_immutable BEFORE UPDATE OR DELETE ON review_policy_snapshots FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER publication_backend_immutable BEFORE UPDATE OR DELETE ON publication_backend_epochs FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION guard_publication_config_head() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.row_version<>OLD.row_version+1 OR NEW.space_id<>OLD.space_id OR NEW.kb_id<>OLD.kb_id
 THEN RAISE EXCEPTION 'configuration head requires next version' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER review_policy_head_guard BEFORE UPDATE ON review_policy_heads FOR EACH ROW EXECUTE FUNCTION guard_publication_config_head();
CREATE TRIGGER publication_backend_head_guard BEFORE UPDATE ON publication_backend_heads FOR EACH ROW EXECUTE FUNCTION guard_publication_config_head();
ALTER TABLE review_decisions ADD UNIQUE(space_id,kb_id,document_id,revision_id,id);
CREATE TABLE publication_intents(
 id platform_id PRIMARY KEY,space_id platform_id NOT NULL,kb_id platform_id NOT NULL,document_id platform_id NOT NULL,revision_id platform_id NOT NULL,approval_id platform_id NOT NULL,
 expected_epoch uint64_value NOT NULL CHECK(expected_epoch<18446744073709551615),actor_id platform_id NOT NULL,command_key platform_id NOT NULL,request_digest sha256_value NOT NULL,
 review_policy_version uint64_value NOT NULL,backend_epoch_id platform_id NOT NULL,job_id platform_id NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=262144),receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 UNIQUE(space_id,actor_id,command_key),UNIQUE(space_id,kb_id,id),UNIQUE(space_id,kb_id,id,job_id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id,approval_id) REFERENCES review_decisions(space_id,kb_id,document_id,revision_id,id),
 FOREIGN KEY(space_id,kb_id,review_policy_version) REFERENCES review_policy_snapshots(space_id,kb_id,version),
 FOREIGN KEY(space_id,kb_id,backend_epoch_id) REFERENCES publication_backend_epochs(space_id,kb_id,id),
 CHECK(coalesce(payload->'Result'=receipt AND receipt->'Operation'->>'ID'=id AND receipt->'Operation'->>'DocumentID'=document_id AND receipt->'Operation'->>'RevisionID'=revision_id AND receipt->'Operation'->>'ApprovalID'=approval_id AND receipt->'Operation'->>'State'='preparing' AND (receipt->'Operation'->>'ExpectedEpoch')::numeric=expected_epoch AND receipt->'Operation'->>'ActorID'=actor_id AND receipt->'Job'->>'ID'=job_id,false))
);
CREATE TABLE publication_queued_jobs(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,id platform_id NOT NULL,operation_id platform_id NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),PRIMARY KEY(space_id,kb_id,id),UNIQUE(space_id,kb_id,operation_id),
 FOREIGN KEY(space_id,kb_id,operation_id,id) REFERENCES publication_intents(space_id,kb_id,id,job_id),
 CHECK(coalesce(payload->>'ID'=id AND payload->>'OperationID'=operation_id AND payload->>'State'='queued' AND payload->>'Stage'='publication.prepare' AND (payload->>'RowVersion')::numeric=1,false))
);
CREATE TABLE publication_start_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,actor_id platform_id NOT NULL,request_id platform_id NOT NULL,
 action text NOT NULL DEFAULT 'publication.start' CHECK(action='publication.start'),occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,operation_id),FOREIGN KEY(space_id,kb_id,operation_id) REFERENCES publication_intents(space_id,kb_id,id)
);
CREATE TABLE publication_prepare_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,job_id platform_id NOT NULL,event_type text NOT NULL CHECK(event_type='publication.prepare.requested'),
 PRIMARY KEY(space_id,kb_id,operation_id),FOREIGN KEY(space_id,kb_id,operation_id,job_id) REFERENCES publication_intents(space_id,kb_id,id,job_id)
);
CREATE TRIGGER publication_intent_immutable BEFORE UPDATE OR DELETE ON publication_intents FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER publication_initial_job_immutable BEFORE UPDATE OR DELETE ON publication_queued_jobs FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER publication_start_audit_immutable BEFORE UPDATE OR DELETE ON publication_start_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION require_publication_start_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_queued_jobs WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND operation_id=NEW.id AND id=NEW.job_id AND payload=NEW.receipt->'Job')
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND id=NEW.job_id AND row_version=1 AND state='queued' AND payload=NEW.receipt->'Job')
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_start_audit WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND operation_id=NEW.id AND actor_id=NEW.actor_id)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_prepare_outbox WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND operation_id=NEW.id AND job_id=NEW.job_id)
 THEN RAISE EXCEPTION 'publication intent requires atomic job, audit and outbox' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER publication_start_effects AFTER INSERT ON publication_intents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_publication_start_effects();
CREATE TABLE publication_task_heads(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,id platform_id NOT NULL,row_version uint64_value NOT NULL CHECK(row_version>0),
 state text NOT NULL CHECK(state IN('queued','running','failed','completed')),attempt uint64_value NOT NULL,fencing_token uint64_value NOT NULL,
 -- utc_nanos intentionally rejects NULL. Queued/failed tasks have no lease;
 -- validate the strict domain only for a present expiry, without weakening it.
 worker_id platform_id,attempt_id platform_id,lease_until text CHECK(CASE WHEN lease_until IS NULL THEN true ELSE lease_until::utc_nanos IS NOT NULL END),payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=32768),
 PRIMARY KEY(space_id,kb_id,id),FOREIGN KEY(space_id,kb_id,id) REFERENCES publication_queued_jobs(space_id,kb_id,id),
 CHECK((state='running' AND worker_id IS NOT NULL AND attempt_id IS NOT NULL AND lease_until IS NOT NULL AND attempt>0 AND fencing_token>0) OR (state<>'running' AND worker_id IS NULL AND attempt_id IS NULL AND lease_until IS NULL)),
 CHECK(state<>'queued' OR (attempt=0 AND fencing_token=0)),
 CHECK(coalesce(payload->>'ID'=id AND payload->>'SpaceID'=space_id AND payload->>'State'=state AND (payload->>'RowVersion')::numeric=row_version AND (payload->>'Attempt')::numeric=attempt AND (payload->>'FencingToken')::numeric=fencing_token,false))
);
CREATE TABLE publication_task_receipts(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,job_id platform_id NOT NULL,worker_id platform_id NOT NULL,kind text NOT NULL CHECK(kind IN('claim','renew')),command_id platform_id NOT NULL,request_id platform_id NOT NULL,
 request_digest sha256_value NOT NULL,status text NOT NULL CHECK(status IN('granted','superseded')),job_version uint64_value NOT NULL CHECK(job_version>1),result jsonb NOT NULL CHECK(jsonb_typeof(result)='object'),
 PRIMARY KEY(space_id,worker_id,kind,command_id),UNIQUE(space_id,kb_id,job_id,job_version),
 FOREIGN KEY(space_id,kb_id,job_id) REFERENCES publication_task_heads(space_id,kb_id,id),
 CHECK(coalesce((result->>'JobVersion')::numeric=job_version,false)),
 CHECK(status='superseded' OR coalesce(result->'Lease'->>'JobID'=job_id AND result->'Lease'->>'WorkerID'=worker_id AND (result->'Lease'->>'FencingToken')::numeric>0,false))
);
CREATE TABLE publication_task_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,job_id platform_id NOT NULL,job_version uint64_value NOT NULL,
 worker_id platform_id NOT NULL,kind text NOT NULL CHECK(kind IN('claim','renew')),command_id platform_id NOT NULL,request_id platform_id NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,job_id,job_version),FOREIGN KEY(space_id,kb_id,job_id,job_version) REFERENCES publication_task_receipts(space_id,kb_id,job_id,job_version)
);
CREATE TABLE publication_task_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,job_id platform_id NOT NULL,job_version uint64_value NOT NULL,kind text NOT NULL CHECK(kind IN('claim','renew')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),PRIMARY KEY(space_id,kb_id,job_id,job_version),
 FOREIGN KEY(space_id,kb_id,job_id,job_version) REFERENCES publication_task_receipts(space_id,kb_id,job_id,job_version)
);
CREATE TRIGGER publication_task_receipt_immutable BEFORE UPDATE OR DELETE ON publication_task_receipts FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER publication_task_audit_immutable BEFORE UPDATE OR DELETE ON publication_task_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION guard_publication_task_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.row_version<>OLD.row_version+1 OR (NEW.space_id,NEW.kb_id,NEW.id) IS DISTINCT FROM (OLD.space_id,OLD.kb_id,OLD.id)
 OR NEW.payload-ARRAY['RowVersion','State','Attempt','FencingToken','LeaseUntil','Error','Progress','GenerationID'] IS DISTINCT FROM OLD.payload-ARRAY['RowVersion','State','Attempt','FencingToken','LeaseUntil','Error','Progress','GenerationID']
 OR (NEW.state<>'completed' AND (NEW.payload->'Progress' IS DISTINCT FROM OLD.payload->'Progress' OR NEW.payload->'GenerationID' IS DISTINCT FROM OLD.payload->'GenerationID'))
 OR NOT(
   (OLD.state IN('queued','running') AND NEW.state='failed' AND NEW.attempt=OLD.attempt AND NEW.fencing_token=OLD.fencing_token+1)
   OR (OLD.state IN('queued','running') AND NEW.state='running' AND (OLD.state='queued' OR OLD.lease_until::timestamptz<=clock_timestamp()) AND NEW.attempt=OLD.attempt+1 AND NEW.fencing_token=OLD.fencing_token+1 AND NEW.attempt_id IS DISTINCT FROM OLD.attempt_id AND NEW.lease_until::timestamptz>clock_timestamp())
   OR (OLD.state='running' AND NEW.state='running' AND OLD.lease_until::timestamptz>clock_timestamp() AND NEW.attempt=OLD.attempt AND NEW.fencing_token=OLD.fencing_token AND NEW.worker_id=OLD.worker_id AND NEW.attempt_id=OLD.attempt_id AND NEW.lease_until::timestamptz>OLD.lease_until::timestamptz)
   OR (OLD.state='running' AND NEW.state='completed' AND OLD.lease_until::timestamptz>clock_timestamp() AND NEW.attempt=OLD.attempt AND NEW.fencing_token=OLD.fencing_token AND NEW.payload->>'LeaseUntil' IS NULL AND NEW.payload->'Error'='null'::jsonb AND (NEW.payload->>'Progress')::numeric=1 AND NEW.payload->>'GenerationID' IS NOT NULL)
 ) THEN RAISE EXCEPTION 'invalid publication task transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER publication_task_transition BEFORE UPDATE ON publication_task_heads FOR EACH ROW EXECUTE FUNCTION guard_publication_task_transition();
CREATE FUNCTION require_publication_task_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND id=NEW.job_id AND row_version=NEW.job_version)
 OR (NEW.status='granted' AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads h WHERE h.space_id=NEW.space_id AND h.kb_id=NEW.kb_id AND h.id=NEW.job_id AND h.row_version=NEW.job_version AND h.state='running' AND h.worker_id=NEW.worker_id AND h.attempt_id=NEW.result->'Lease'->>'AttemptID' AND h.fencing_token=(NEW.result->'Lease'->>'FencingToken')::numeric AND h.payload->>'LeaseUntil'=NEW.result->'Lease'->>'ExpiresAt'))
 OR (NEW.status='superseded' AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads h WHERE h.space_id=NEW.space_id AND h.kb_id=NEW.kb_id AND h.id=NEW.job_id AND h.row_version=NEW.job_version AND h.state='failed' AND h.worker_id IS NULL AND h.lease_until IS NULL AND coalesce(NEW.result->'Lease'->>'JobID'='' AND NEW.result->'Lease'->>'AttemptID'='' AND (NEW.result->'Lease'->>'FencingToken')::numeric=0,false)))
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_audit a JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=a.space_id AND h.kb_id=a.kb_id AND h.id=a.job_id AND h.row_version=a.job_version WHERE a.space_id=NEW.space_id AND a.kb_id=NEW.kb_id AND a.job_id=NEW.job_id AND a.job_version=NEW.job_version AND a.worker_id=NEW.worker_id AND a.kind=NEW.kind AND a.command_id=NEW.command_id AND a.request_id=NEW.request_id AND a.payload=h.payload)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_outbox o JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=o.space_id AND h.kb_id=o.kb_id AND h.id=o.job_id AND h.row_version=o.job_version WHERE o.space_id=NEW.space_id AND o.kb_id=NEW.kb_id AND o.job_id=NEW.job_id AND o.job_version=NEW.job_version AND o.kind=NEW.kind AND o.payload=h.payload)
 THEN RAISE EXCEPTION 'publication task requires atomic receipt audit and event' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER publication_task_effects AFTER INSERT ON publication_task_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_publication_task_effects();
-- Physical keyword data is kept in the existing scoped projection store.
ALTER TABLE index_projection_rows ADD COLUMN keyword_vector tsvector GENERATED ALWAYS AS(to_tsvector('simple'::regconfig,coalesce(payload->'Chunk'->>'Text',''))) STORED;
CREATE INDEX index_projection_keyword_gin ON index_projection_rows USING gin(keyword_vector);
CREATE TABLE publication_prepared_artifacts(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,job_id platform_id NOT NULL,attempt_id platform_id NOT NULL,fencing_token uint64_value NOT NULL CHECK(fencing_token>0),
 generation_set_id platform_id NOT NULL,generation_id platform_id NOT NULL,artifact_digest sha256_value NOT NULL,snapshot_digest sha256_value NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=1048576),
 PRIMARY KEY(space_id,kb_id,operation_id,attempt_id),UNIQUE(generation_set_id),UNIQUE(generation_id),
 FOREIGN KEY(space_id,kb_id,operation_id,job_id) REFERENCES publication_intents(space_id,kb_id,id,job_id),
 FOREIGN KEY(space_id,kb_id,job_id) REFERENCES publication_task_heads(space_id,kb_id,id),
 FOREIGN KEY(generation_id) REFERENCES index_generations(id),
 CHECK(coalesce(payload->>'OperationID'=operation_id AND payload->>'JobID'=job_id AND payload->>'AttemptID'=attempt_id AND (payload->>'FencingToken')::numeric=fencing_token AND payload->>'Digest'=artifact_digest AND payload->>'SnapshotDigest'=snapshot_digest AND payload->'GenerationSet'->>'ID'=generation_set_id,false))
);
CREATE TABLE publication_preparation_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,attempt_id platform_id NOT NULL,worker_id platform_id NOT NULL,request_id platform_id NOT NULL,
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),PRIMARY KEY(space_id,kb_id,operation_id,attempt_id),
 FOREIGN KEY(space_id,kb_id,operation_id,attempt_id) REFERENCES publication_prepared_artifacts(space_id,kb_id,operation_id,attempt_id)
);
CREATE TABLE publication_preparation_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,attempt_id platform_id NOT NULL,artifact_digest sha256_value NOT NULL,
 event_type text NOT NULL DEFAULT 'publication.index.prepared' CHECK(event_type='publication.index.prepared'),PRIMARY KEY(space_id,kb_id,operation_id,attempt_id),
 FOREIGN KEY(space_id,kb_id,operation_id,attempt_id) REFERENCES publication_prepared_artifacts(space_id,kb_id,operation_id,attempt_id)
);
CREATE TRIGGER prepared_artifact_immutable BEFORE UPDATE OR DELETE ON publication_prepared_artifacts FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER preparation_audit_immutable BEFORE UPDATE OR DELETE ON publication_preparation_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION guard_sealed_preparation_rows() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE generation text;
BEGIN
 generation:=CASE WHEN TG_OP='DELETE' THEN OLD.generation_id ELSE NEW.generation_id END;
 IF TG_OP='UPDATE' AND EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_prepared_artifacts WHERE generation_id=OLD.generation_id)
 THEN RAISE EXCEPTION 'sealed preparation cannot move to another generation' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_prepared_artifacts WHERE generation_id=generation)
 AND NOT(TG_OP='DELETE' AND EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences WHERE generation_id=generation))
 THEN RAISE EXCEPTION 'sealed preparation projection cannot change' USING ERRCODE='23514'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$;
CREATE TRIGGER sealed_preparation_rows BEFORE INSERT OR UPDATE OR DELETE ON index_projection_rows FOR EACH ROW EXECUTE FUNCTION guard_sealed_preparation_rows();
CREATE FUNCTION require_preparation_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_preparation_audit WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND operation_id=NEW.operation_id AND attempt_id=NEW.attempt_id)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_preparation_outbox WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND operation_id=NEW.operation_id AND attempt_id=NEW.attempt_id AND artifact_digest=NEW.artifact_digest)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_generations g JOIN akos_withdraw_v1.generation_sets s ON s.id=g.generation_set_id JOIN akos_withdraw_v1.publication_intents i ON i.id=NEW.operation_id JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=i.space_id AND h.kb_id=i.kb_id AND h.id=i.job_id JOIN akos_withdraw_v1.publication_preparation_audit a ON a.space_id=i.space_id AND a.kb_id=i.kb_id AND a.operation_id=i.id AND a.attempt_id=NEW.attempt_id WHERE g.id=NEW.generation_id AND g.generation_set_id=NEW.generation_set_id AND g.space_id=NEW.space_id AND g.kb_id=NEW.kb_id AND g.revision_id=s.revision_id AND g.document_id=i.document_id AND g.revision_id=i.revision_id AND g.backend_epoch_id=i.backend_epoch_id AND g.profile_digest=NEW.payload->'Profile'->>'Digest' AND h.state='running' AND h.attempt_id=NEW.attempt_id AND h.fencing_token=NEW.fencing_token AND h.worker_id=a.worker_id AND (SELECT count(*) FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=g.id)=(NEW.payload->'GenerationSet'->'Channels'->'keyword'->'Readiness'->'Generations'->0->>'ChunkCount')::integer)
 THEN RAISE EXCEPTION 'preparation requires matching generation audit and event' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER preparation_effects AFTER INSERT ON publication_prepared_artifacts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_preparation_effects();
CREATE TABLE publication_completions(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,job_id platform_id NOT NULL,worker_id platform_id NOT NULL,command_id platform_id NOT NULL,request_id platform_id NOT NULL,request_digest sha256_value NOT NULL,
 operation_id platform_id NOT NULL,attempt_id platform_id NOT NULL,fencing_token uint64_value NOT NULL CHECK(fencing_token>0),artifact_digest sha256_value NOT NULL,job_version uint64_value NOT NULL CHECK(job_version>1),
 lease_until utc_nanos NOT NULL,completed_at utc_nanos NOT NULL,result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=2097152),
 PRIMARY KEY(space_id,worker_id,command_id),UNIQUE(space_id,kb_id,job_id),
 FOREIGN KEY(space_id,kb_id,job_id) REFERENCES publication_task_heads(space_id,kb_id,id),
 FOREIGN KEY(space_id,kb_id,operation_id,attempt_id) REFERENCES publication_prepared_artifacts(space_id,kb_id,operation_id,attempt_id),
 CHECK(completed_at::timestamptz<lease_until::timestamptz),
 CHECK(coalesce(result->'Job'->>'ID'=job_id AND result->'Job'->>'OperationID'=operation_id AND result->'Job'->>'State'='completed' AND (result->'Job'->>'RowVersion')::numeric=job_version AND (result->'Job'->>'FencingToken')::numeric=fencing_token AND result->'Artifact'->>'OperationID'=operation_id AND result->'Artifact'->>'JobID'=job_id AND result->'Artifact'->>'AttemptID'=attempt_id AND result->'Artifact'->>'Digest'=artifact_digest AND (result->'Artifact'->>'FencingToken')::numeric=fencing_token AND (result->>'CompletedAt')::timestamptz=completed_at::timestamptz,false))
);
CREATE TABLE publication_completion_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,job_id platform_id NOT NULL,worker_id platform_id NOT NULL,command_id platform_id NOT NULL,artifact_digest sha256_value NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),PRIMARY KEY(space_id,kb_id,job_id),
 FOREIGN KEY(space_id,worker_id,command_id) REFERENCES publication_completions(space_id,worker_id,command_id)
);
CREATE TABLE publication_completion_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,job_id platform_id NOT NULL,worker_id platform_id NOT NULL,command_id platform_id NOT NULL,artifact_digest sha256_value NOT NULL,
 event_type text NOT NULL DEFAULT 'publication.prepare.completed' CHECK(event_type='publication.prepare.completed'),payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),PRIMARY KEY(space_id,kb_id,job_id),
 FOREIGN KEY(space_id,worker_id,command_id) REFERENCES publication_completions(space_id,worker_id,command_id)
);
CREATE TRIGGER publication_completion_immutable BEFORE UPDATE OR DELETE ON publication_completions FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER completion_audit_immutable BEFORE UPDATE OR DELETE ON publication_completion_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION require_publication_completion_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads h JOIN akos_withdraw_v1.publication_prepared_artifacts a ON a.space_id=h.space_id AND a.kb_id=h.kb_id AND a.job_id=h.id AND a.operation_id=NEW.operation_id AND a.attempt_id=NEW.attempt_id WHERE h.space_id=NEW.space_id AND h.kb_id=NEW.kb_id AND h.id=NEW.job_id AND h.state='completed' AND h.row_version=NEW.job_version AND h.fencing_token=NEW.fencing_token AND h.payload=NEW.result->'Job' AND a.payload=NEW.result->'Artifact' AND a.artifact_digest=NEW.artifact_digest AND h.payload->>'GenerationID'=a.generation_id)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_completion_audit a WHERE a.space_id=NEW.space_id AND a.kb_id=NEW.kb_id AND a.job_id=NEW.job_id AND a.worker_id=NEW.worker_id AND a.command_id=NEW.command_id AND a.artifact_digest=NEW.artifact_digest AND a.payload=NEW.result->'Job')
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_completion_outbox a WHERE a.space_id=NEW.space_id AND a.kb_id=NEW.kb_id AND a.job_id=NEW.job_id AND a.worker_id=NEW.worker_id AND a.command_id=NEW.command_id AND a.artifact_digest=NEW.artifact_digest AND a.payload=NEW.result->'Job')
 THEN RAISE EXCEPTION 'completion requires matching artifact, head, audit and event' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER publication_completion_effects AFTER INSERT ON publication_completions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_publication_completion_effects();
CREATE FUNCTION require_completed_task_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='completed' AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_completions c WHERE c.space_id=NEW.space_id AND c.kb_id=NEW.kb_id AND c.job_id=NEW.id AND c.job_version=NEW.row_version AND c.result->'Job'=NEW.payload)
 THEN RAISE EXCEPTION 'completed task requires completion receipt' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER completed_task_receipt AFTER UPDATE ON publication_task_heads DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_completed_task_receipt();
CREATE TABLE publication_finalizations(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,job_id platform_id NOT NULL,document_id platform_id NOT NULL,history_id platform_id NOT NULL,
 artifact_digest sha256_value NOT NULL,request_digest sha256_value NOT NULL,receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=2097152),
 PRIMARY KEY(space_id,kb_id,operation_id),UNIQUE(history_id),
 FOREIGN KEY(space_id,kb_id,operation_id,job_id) REFERENCES publication_intents(space_id,kb_id,id,job_id),
 FOREIGN KEY(space_id,kb_id,job_id) REFERENCES publication_completions(space_id,kb_id,job_id),
 FOREIGN KEY(space_id,kb_id,document_id,history_id,operation_id) REFERENCES publication_history(space_id,kb_id,document_id,id,operation_id),
 CHECK(coalesce(receipt->'Operation'->>'ID'=operation_id AND receipt->'Operation'->>'State'='committed' AND receipt->'Operation'->>'DocumentID'=document_id AND receipt->'History'->>'ID'=history_id AND receipt->'History'->>'CommandDigest'=request_digest,false))
);
CREATE TABLE publication_publish_audit(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,history_id platform_id NOT NULL,request_id platform_id NOT NULL,actor_id platform_id NOT NULL,
 publication_epoch uint64_value NOT NULL CHECK(publication_epoch>0),cleanup_plan_id platform_id,payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,operation_id),FOREIGN KEY(space_id,kb_id,operation_id) REFERENCES publication_finalizations(space_id,kb_id,operation_id),
 FOREIGN KEY(cleanup_plan_id) REFERENCES cleanup_plans(id)
);
CREATE TABLE publication_publish_outbox(
 space_id platform_id NOT NULL,kb_id platform_id NOT NULL,operation_id platform_id NOT NULL,history_id platform_id NOT NULL,request_id platform_id NOT NULL,actor_id platform_id NOT NULL,
 publication_epoch uint64_value NOT NULL CHECK(publication_epoch>0),cleanup_plan_id platform_id,event_type text NOT NULL DEFAULT 'publication.published' CHECK(event_type='publication.published'),payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(space_id,kb_id,operation_id),FOREIGN KEY(space_id,kb_id,operation_id) REFERENCES publication_finalizations(space_id,kb_id,operation_id),
 FOREIGN KEY(cleanup_plan_id) REFERENCES cleanup_plans(id)
);
CREATE TRIGGER finalization_immutable BEFORE UPDATE OR DELETE ON publication_finalizations FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER publication_publish_audit_immutable BEFORE UPDATE OR DELETE ON publication_publish_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE FUNCTION require_publication_finalization_effects() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE h akos_withdraw_v1.publication_history%ROWTYPE;
BEGIN
 SELECT * INTO h FROM akos_withdraw_v1.publication_history WHERE id=NEW.history_id;
 IF NOT FOUND OR h.action<>'publish' OR h.command_digest<>NEW.request_digest
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.documents d WHERE d.space_id=NEW.space_id AND d.kb_id=NEW.kb_id AND d.id=NEW.document_id AND NOT d.deleted AND d.publication_epoch=h.publication_epoch AND d.active_revision_id=h.revision_id AND d.active_generation_set_id=h.generation_set_id AND d.row_version=(NEW.receipt->>'DocumentRowVersion')::numeric)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_completions c JOIN akos_withdraw_v1.publication_intents i ON i.id=NEW.operation_id JOIN akos_withdraw_v1.command_operations o ON o.id=i.id WHERE c.space_id=NEW.space_id AND c.kb_id=NEW.kb_id AND c.job_id=NEW.job_id AND c.operation_id=i.id AND c.artifact_digest=NEW.artifact_digest AND o.state='committed' AND o.action='publish' AND o.command_digest=NEW.request_digest AND o.history_id=h.id AND h.revision_id=i.revision_id AND h.generation_set_id=c.result->'Artifact'->'GenerationSet'->>'ID' AND NEW.receipt->'History'->>'OperationID'=i.id AND NEW.receipt->'Operation'->>'RevisionID'=i.revision_id AND NEW.receipt->'Operation'->>'ApprovalID'=i.approval_id AND NEW.receipt->'Operation'->>'GenerationSetID'=h.generation_set_id AND (NEW.receipt->'Operation'->>'ExpectedEpoch')::numeric=h.previous_epoch)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_publish_audit a WHERE a.space_id=NEW.space_id AND a.kb_id=NEW.kb_id AND a.operation_id=NEW.operation_id AND a.history_id=h.id AND a.actor_id=h.actor_id AND a.publication_epoch=h.publication_epoch AND a.payload=NEW.receipt AND a.cleanup_plan_id IS NOT DISTINCT FROM NEW.receipt->>'CleanupPlanID')
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_publish_outbox a WHERE a.space_id=NEW.space_id AND a.kb_id=NEW.kb_id AND a.operation_id=NEW.operation_id AND a.history_id=h.id AND a.actor_id=h.actor_id AND a.publication_epoch=h.publication_epoch AND a.payload=NEW.receipt AND a.cleanup_plan_id IS NOT DISTINCT FROM NEW.receipt->>'CleanupPlanID')
 OR (h.previous_revision_id IS NULL AND NEW.receipt->>'CleanupPlanID' IS NOT NULL)
 OR (h.previous_revision_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_plans p WHERE p.id=NEW.receipt->>'CleanupPlanID' AND p.history_id=h.id AND p.operation_id=h.operation_id AND p.history_action='publish'))
 THEN RAISE EXCEPTION 'publication requires atomic matching history, head, completion, cleanup, audit and event' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER publication_finalization_effects AFTER INSERT ON publication_finalizations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_publication_finalization_effects();
CREATE FUNCTION require_prepared_publication_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.active_generation_set_id IS NOT NULL AND EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_prepared_artifacts a WHERE a.generation_set_id=NEW.active_generation_set_id)
 AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_finalizations f JOIN akos_withdraw_v1.publication_history h ON h.id=f.history_id WHERE f.space_id=NEW.space_id AND f.kb_id=NEW.kb_id AND f.document_id=NEW.id AND h.revision_id=NEW.active_revision_id AND h.generation_set_id=NEW.active_generation_set_id AND h.publication_epoch=NEW.publication_epoch)
 THEN RAISE EXCEPTION 'prepared source activation requires publication receipt' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER prepared_publication_receipt AFTER UPDATE ON documents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_prepared_publication_receipt();
-- Bootstrap/restore control must initialize and rotate this identity explicitly.
-- A database snapshot or connection identifier is not a restored-store identity.
CREATE TABLE query_store_head(
 singleton boolean PRIMARY KEY CHECK(singleton),store_epoch_id platform_id NOT NULL UNIQUE,row_version uint64_value NOT NULL CHECK(row_version>0)
);
CREATE FUNCTION guard_query_store_head() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.singleton<>OLD.singleton OR NEW.row_version<>OLD.row_version+1 OR NEW.store_epoch_id=OLD.store_epoch_id
 THEN RAISE EXCEPTION 'store identity rotation requires new epoch and next version' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER query_store_head_guard BEFORE UPDATE ON query_store_head FOR EACH ROW EXECUTE FUNCTION guard_query_store_head();
COMMIT;

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
 history_action text NOT NULL DEFAULT 'withdraw' CHECK(history_action='withdraw'),
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
COMMIT;

-- Add a durable paused publication preparation state and preserve fenced
-- control receipts for existing withdraw_contract_v1 installations.
BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

ALTER TABLE publication_task_heads DROP CONSTRAINT publication_task_heads_state_check;
ALTER TABLE publication_task_heads ADD CONSTRAINT publication_task_heads_state_check CHECK(state IN('queued','running','paused','failed','completed'));

CREATE OR REPLACE FUNCTION guard_publication_task_transition() RETURNS trigger LANGUAGE plpgsql AS $$ 
BEGIN
 IF NEW.row_version<>OLD.row_version+1 OR (NEW.space_id,NEW.kb_id,NEW.id) IS DISTINCT FROM (OLD.space_id,OLD.kb_id,OLD.id)
 OR NEW.payload-ARRAY['RowVersion','State','Attempt','FencingToken','LeaseUntil','Error','Progress','GenerationID'] IS DISTINCT FROM OLD.payload-ARRAY['RowVersion','State','Attempt','FencingToken','LeaseUntil','Error','Progress','GenerationID']
 OR (NEW.state NOT IN('completed') AND (NEW.payload->'Progress' IS DISTINCT FROM OLD.payload->'Progress' OR NEW.payload->'GenerationID' IS DISTINCT FROM OLD.payload->'GenerationID'))
 OR NOT(
   (OLD.state IN('queued','running') AND NEW.state IN('failed','paused') AND NEW.attempt=OLD.attempt AND NEW.fencing_token=OLD.fencing_token+1)
   OR (OLD.state IN('queued','running') AND NEW.state='running' AND (OLD.state='queued' OR OLD.lease_until::timestamptz<=clock_timestamp()) AND NEW.attempt=OLD.attempt+1 AND NEW.fencing_token=OLD.fencing_token+1 AND NEW.attempt_id IS DISTINCT FROM OLD.attempt_id AND NEW.lease_until::timestamptz>clock_timestamp())
   OR (OLD.state='running' AND NEW.state='running' AND OLD.lease_until::timestamptz>clock_timestamp() AND NEW.attempt=OLD.attempt AND NEW.fencing_token=OLD.fencing_token AND NEW.worker_id=OLD.worker_id AND NEW.attempt_id=OLD.attempt_id AND NEW.lease_until::timestamptz>OLD.lease_until::timestamptz)
   OR (OLD.state='running' AND NEW.state='completed' AND OLD.lease_until::timestamptz>clock_timestamp() AND NEW.attempt=OLD.attempt AND NEW.fencing_token=OLD.fencing_token AND NEW.payload->>'LeaseUntil' IS NULL AND NEW.payload->'Error'='null'::jsonb AND (NEW.payload->>'Progress')::numeric=1 AND NEW.payload->>'GenerationID' IS NOT NULL)
 ) THEN RAISE EXCEPTION 'invalid publication task transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION require_publication_task_effects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads WHERE space_id=NEW.space_id AND kb_id=NEW.kb_id AND id=NEW.job_id AND row_version=NEW.job_version)
 OR (NEW.status='granted' AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads h WHERE h.space_id=NEW.space_id AND h.kb_id=NEW.kb_id AND h.id=NEW.job_id AND h.row_version=NEW.job_version AND h.state='running' AND h.worker_id=NEW.worker_id AND h.attempt_id=NEW.result->'Lease'->>'AttemptID' AND h.fencing_token=(NEW.result->'Lease'->>'FencingToken')::numeric AND h.payload->>'LeaseUntil'=NEW.result->'Lease'->>'ExpiresAt'))
 OR (NEW.status='superseded' AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_heads h WHERE h.space_id=NEW.space_id AND h.kb_id=NEW.kb_id AND h.id=NEW.job_id AND h.row_version=NEW.job_version AND h.state IN('failed','paused') AND h.worker_id IS NULL AND h.lease_until IS NULL AND coalesce(NEW.result->'Lease'->>'JobID'='' AND NEW.result->'Lease'->>'AttemptID'='' AND (NEW.result->'Lease'->>'FencingToken')::numeric=0,false)))
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_audit a JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=a.space_id AND h.kb_id=a.kb_id AND h.id=a.job_id AND h.row_version=a.job_version WHERE a.space_id=NEW.space_id AND a.kb_id=NEW.kb_id AND a.job_id=NEW.job_id AND a.job_version=NEW.job_version AND a.worker_id=NEW.worker_id AND a.kind=NEW.kind AND a.command_id=NEW.command_id AND a.request_id=NEW.request_id AND a.payload=h.payload)
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_task_outbox o JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=o.space_id AND h.kb_id=o.kb_id AND h.id=o.job_id AND h.row_version=o.job_version WHERE o.space_id=NEW.space_id AND o.kb_id=NEW.kb_id AND o.job_id=NEW.job_id AND o.job_version=NEW.job_version AND o.kind=NEW.kind AND o.payload=h.payload)
 THEN RAISE EXCEPTION 'publication task requires atomic receipt audit and event' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;

COMMIT;
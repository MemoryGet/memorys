-- Incremental parser task persistence for an existing withdraw_contract_v1 schema.
BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

CREATE TABLE parse_task_heads(
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 id platform_id NOT NULL,
 row_version uint64_value NOT NULL CHECK(row_version>0),
 state text NOT NULL CHECK(state IN('queued','running','paused','completed','failed','cancelled')),
 attempt uint64_value NOT NULL CHECK(attempt>0),
 fencing_token uint64_value NOT NULL,
 worker_id platform_id,
 attempt_id platform_id,
 lease_until timestamptz,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=262144),
 error jsonb,
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,id),
 CHECK((state='running')=(worker_id IS NOT NULL AND attempt_id IS NOT NULL AND lease_until IS NOT NULL)),
 CHECK(state<>'running' OR fencing_token>0),
 CHECK(jsonb_typeof(error)='object' OR error IS NULL)
);

CREATE TABLE parse_task_audit(
 id platform_id PRIMARY KEY,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 task_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 request_id platform_id NOT NULL,
 action text NOT NULL CHECK(action IN('parse.task.created','parse.task.paused','parse.task.cancelled','parse.task.retry_requested')),
 row_version uint64_value NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 FOREIGN KEY(space_id,kb_id,task_id) REFERENCES parse_task_heads(space_id,kb_id,id)
);

CREATE TABLE parse_task_outbox(
 id platform_id PRIMARY KEY,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 task_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type IN('parse.task.created','parse.task.paused','parse.task.cancelled','parse.task.retry_requested')),
 row_version uint64_value NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 FOREIGN KEY(space_id,kb_id,task_id) REFERENCES parse_task_heads(space_id,kb_id,id)
);

COMMIT;

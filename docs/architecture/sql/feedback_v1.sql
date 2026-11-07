-- Incremental feedback persistence for an existing withdraw_contract_v1 schema.
BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

CREATE TABLE feedback_records(
 id platform_id PRIMARY KEY,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 document_id platform_id,
 revision_id platform_id,
 block_id platform_id,
 target_request_id platform_id,
 actor_id platform_id NOT NULL,
 request_id platform_id NOT NULL,
 idempotency_key platform_id NOT NULL,
 request_digest sha256_value NOT NULL,
 citation jsonb NOT NULL CHECK(jsonb_typeof(citation)='object' OR citation='null'::jsonb),
 kind text NOT NULL CHECK(kind IN('incorrect','missing','outdated','helpful','other')),
 comment text NOT NULL CHECK(char_length(comment) BETWEEN 1 AND 10000),
 state text NOT NULL CHECK(state='received'),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(space_id,actor_id,idempotency_key),
 FOREIGN KEY(space_id,kb_id,document_id) REFERENCES documents(space_id,kb_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id) REFERENCES revision_contents(space_id,kb_id,document_id,revision_id),
 CHECK((document_id IS NOT NULL AND revision_id IS NOT NULL AND block_id IS NOT NULL AND target_request_id IS NULL) OR
       (document_id IS NULL AND revision_id IS NULL AND block_id IS NULL AND target_request_id IS NOT NULL))
);

CREATE TABLE feedback_audit(
 id platform_id PRIMARY KEY,
 feedback_id platform_id NOT NULL UNIQUE,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 request_id platform_id NOT NULL,
 action text NOT NULL CHECK(action='feedback.submit'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 FOREIGN KEY(feedback_id) REFERENCES feedback_records(id)
);

CREATE TABLE feedback_outbox(
 id platform_id PRIMARY KEY,
 feedback_id platform_id NOT NULL UNIQUE,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type='feedback.received'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 FOREIGN KEY(feedback_id) REFERENCES feedback_records(id)
);

CREATE TRIGGER feedback_immutable BEFORE UPDATE OR DELETE ON feedback_records FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER feedback_audit_immutable BEFORE UPDATE OR DELETE ON feedback_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER feedback_outbox_immutable BEFORE UPDATE OR DELETE ON feedback_outbox FOR EACH ROW EXECUTE FUNCTION immutable_record();

COMMIT;

BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

CREATE TABLE knowledge_base_metadata(
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 10000),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id)
);
CREATE TABLE knowledge_base_create_receipts(
 id platform_id PRIMARY KEY,
 space_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 command_key platform_id NOT NULL,
 request_digest sha256_value NOT NULL,
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(space_id,actor_id,command_key)
);
CREATE TABLE knowledge_base_create_audit(
 id platform_id PRIMARY KEY,
 receipt_id platform_id NOT NULL UNIQUE REFERENCES knowledge_base_create_receipts(id),
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 request_id platform_id NOT NULL,
 action text NOT NULL CHECK(action='knowledge_base.created'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TABLE knowledge_base_create_outbox(
 id platform_id PRIMARY KEY,
 receipt_id platform_id NOT NULL UNIQUE REFERENCES knowledge_base_create_receipts(id),
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type='knowledge_base.created'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object')
);
CREATE TRIGGER knowledge_base_metadata_immutable BEFORE UPDATE OR DELETE ON knowledge_base_metadata FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER knowledge_base_create_receipt_immutable BEFORE UPDATE OR DELETE ON knowledge_base_create_receipts FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER knowledge_base_create_audit_immutable BEFORE UPDATE OR DELETE ON knowledge_base_create_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER knowledge_base_create_outbox_immutable BEFORE UPDATE OR DELETE ON knowledge_base_create_outbox FOR EACH ROW EXECUTE FUNCTION immutable_record();

COMMIT;

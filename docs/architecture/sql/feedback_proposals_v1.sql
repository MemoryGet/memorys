BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

CREATE TABLE feedback_proposals(
 id platform_id PRIMARY KEY,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 feedback_id platform_id NOT NULL,
 document_id platform_id NOT NULL,
 revision_id platform_id NOT NULL,
 draft_id platform_id NOT NULL UNIQUE,
 actor_id platform_id NOT NULL,
 request_id platform_id NOT NULL,
 idempotency_key platform_id NOT NULL,
 request_digest sha256_value NOT NULL,
 comment text NOT NULL CHECK(char_length(comment) BETWEEN 1 AND 10000),
 state text NOT NULL CHECK(state='proposed'),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(space_id,actor_id,idempotency_key),
 FOREIGN KEY(feedback_id) REFERENCES feedback_records(id),
 FOREIGN KEY(space_id,kb_id,document_id) REFERENCES documents(space_id,kb_id,id),
 FOREIGN KEY(space_id,kb_id,document_id,revision_id) REFERENCES revision_contents(space_id,kb_id,document_id,revision_id),
 FOREIGN KEY(space_id,kb_id,document_id,draft_id) REFERENCES draft_heads(space_id,kb_id,document_id,id)
);

CREATE TABLE feedback_proposal_audit(
 id platform_id PRIMARY KEY,
 proposal_id platform_id NOT NULL UNIQUE REFERENCES feedback_proposals(id),
 feedback_id platform_id NOT NULL REFERENCES feedback_records(id),
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 request_id platform_id NOT NULL,
 action text NOT NULL CHECK(action='feedback.proposed'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE feedback_proposal_outbox(
 id platform_id PRIMARY KEY,
 proposal_id platform_id NOT NULL UNIQUE REFERENCES feedback_proposals(id),
 feedback_id platform_id NOT NULL REFERENCES feedback_records(id),
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type='feedback.proposed'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object')
);

CREATE TRIGGER feedback_proposals_immutable BEFORE UPDATE OR DELETE ON feedback_proposals FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER feedback_proposal_audit_immutable BEFORE UPDATE OR DELETE ON feedback_proposal_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER feedback_proposal_outbox_immutable BEFORE UPDATE OR DELETE ON feedback_proposal_outbox FOR EACH ROW EXECUTE FUNCTION immutable_record();

COMMIT;

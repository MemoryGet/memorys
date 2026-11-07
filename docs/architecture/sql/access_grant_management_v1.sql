BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

CREATE TABLE access_policy_change_receipts(
 id platform_id PRIMARY KEY,
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 command_key platform_id NOT NULL,
 request_digest sha256_value NOT NULL,
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(space_id,actor_id,command_key)
);

CREATE TABLE access_policy_change_audit(
 id platform_id PRIMARY KEY,
 receipt_id platform_id NOT NULL UNIQUE REFERENCES access_policy_change_receipts(id),
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 actor_id platform_id NOT NULL,
 subject_id platform_id NOT NULL,
 action text NOT NULL CHECK(action='grant.set'),
 policy_version uint64_value NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE access_policy_change_outbox(
 id platform_id PRIMARY KEY,
 receipt_id platform_id NOT NULL UNIQUE REFERENCES access_policy_change_receipts(id),
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 event_type text NOT NULL CHECK(event_type='grant.changed'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object')
);

CREATE TRIGGER access_policy_change_receipt_immutable BEFORE UPDATE OR DELETE ON access_policy_change_receipts FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER access_policy_change_audit_immutable BEFORE UPDATE OR DELETE ON access_policy_change_audit FOR EACH ROW EXECUTE FUNCTION immutable_record();
CREATE TRIGGER access_policy_change_outbox_immutable BEFORE UPDATE OR DELETE ON access_policy_change_outbox FOR EACH ROW EXECUTE FUNCTION immutable_record();

-- Promote existing review administrators into the first management role.
-- The current ACL remains immutable; each space receives one new sealed
-- snapshot and the head advances only after all grants are present.
DO $$
DECLARE
 h record;
 next_version numeric;
BEGIN
 FOR h IN
   SELECT h.space_id,h.version
   FROM access_policy_heads h
   JOIN access_policy_snapshots s ON s.space_id=h.space_id AND s.version=h.version
   WHERE s.sealed
   ORDER BY h.space_id
 LOOP
   next_version := h.version + 1;
   INSERT INTO access_policy_snapshots(space_id,version,sealed)
   VALUES(h.space_id,next_version,false);
   INSERT INTO access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action)
   SELECT space_id,next_version,issuer,subject_id,client_id,kb_id,action
   FROM access_policy_grants
   WHERE space_id=h.space_id AND version=h.version;
   INSERT INTO access_policy_grants(space_id,version,issuer,subject_id,client_id,kb_id,action)
   SELECT g.space_id,next_version,g.issuer,g.subject_id,g.client_id,g.kb_id,a.action
   FROM access_policy_grants g
   CROSS JOIN (VALUES ('kb.manage_access'),('kb.manage_feedback'),('knowledge.read')) AS a(action)
   WHERE g.space_id=h.space_id AND g.version=h.version AND g.action='revision.approve'
     AND NOT EXISTS (
       SELECT 1 FROM access_policy_grants existing
       WHERE existing.space_id=h.space_id AND existing.version=next_version
         AND existing.issuer=g.issuer AND existing.subject_id=g.subject_id
         AND existing.client_id=g.client_id AND existing.kb_id=g.kb_id
         AND existing.action=a.action
     );
   UPDATE access_policy_snapshots SET sealed=true WHERE space_id=h.space_id AND version=next_version;
   UPDATE access_policy_heads SET version=next_version WHERE space_id=h.space_id AND version=h.version;
 END LOOP;
END $$;

COMMIT;

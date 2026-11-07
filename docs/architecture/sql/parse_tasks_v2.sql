-- Add durable parser failure effects without rewriting the applied v1 ledger row.
BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

ALTER TABLE parse_task_audit DROP CONSTRAINT parse_task_audit_action_check;
ALTER TABLE parse_task_audit ADD CONSTRAINT parse_task_audit_action_check CHECK(action IN('parse.task.created','parse.task.failed','parse.task.cancelled','parse.task.retry_requested'));
ALTER TABLE parse_task_outbox DROP CONSTRAINT parse_task_outbox_event_type_check;
ALTER TABLE parse_task_outbox ADD CONSTRAINT parse_task_outbox_event_type_check CHECK(event_type IN('parse.task.created','parse.task.failed','parse.task.cancelled','parse.task.retry_requested'));

COMMIT;

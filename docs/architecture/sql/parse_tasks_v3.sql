-- Add durable parser pause/resume effects without rewriting existing rows.
BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

ALTER TABLE parse_task_heads DROP CONSTRAINT parse_task_heads_state_check;
ALTER TABLE parse_task_heads ADD CONSTRAINT parse_task_heads_state_check CHECK(state IN('queued','running','paused','completed','failed','cancelled'));
ALTER TABLE parse_task_audit DROP CONSTRAINT parse_task_audit_action_check;
ALTER TABLE parse_task_audit ADD CONSTRAINT parse_task_audit_action_check CHECK(action IN('parse.task.created','parse.task.failed','parse.task.paused','parse.task.cancelled','parse.task.retry_requested'));
ALTER TABLE parse_task_outbox DROP CONSTRAINT parse_task_outbox_event_type_check;
ALTER TABLE parse_task_outbox ADD CONSTRAINT parse_task_outbox_event_type_check CHECK(event_type IN('parse.task.created','parse.task.failed','parse.task.paused','parse.task.cancelled','parse.task.retry_requested'));

COMMIT;

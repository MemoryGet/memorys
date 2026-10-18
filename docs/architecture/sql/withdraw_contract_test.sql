SET search_path=akos_withdraw_v1,pg_catalog;
SET statement_timeout='10s';
CREATE FUNCTION expect_error(statement text,expected_state text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE observed text;
BEGIN
 BEGIN EXECUTE statement; RAISE EXCEPTION 'statement unexpectedly accepted' USING ERRCODE='P0002';
 EXCEPTION WHEN OTHERS THEN GET STACKED DIAGNOSTICS observed=RETURNED_SQLSTATE;
  IF observed<>expected_state THEN RAISE EXCEPTION 'expected %, observed % for %',expected_state,observed,statement; END IF;
 END;
END; $$;
SELECT expect_error('SELECT 1.5::uint64_value','23514');
SELECT expect_error('SELECT 18446744073709551616::uint64_value','23514');
SELECT expect_error('SELECT (-1)::uint64_value','23514');
DO $$ BEGIN IF (18446744073709551615::uint64_value)::text<>'18446744073709551615' THEN RAISE EXCEPTION 'uint64 precision lost'; END IF; END; $$;
INSERT INTO documents VALUES('space','kb','doc',1,1,NULL,NULL);
INSERT INTO revisions VALUES('space','kb','doc','revision',repeat('a',64));
INSERT INTO generation_sets VALUES('space','kb','doc','revision','set','epoch');
INSERT INTO index_generations VALUES('space','kb','doc','revision','set','generation','epoch','keyword',repeat('a',64),'engine');
UPDATE documents SET active_revision_id='revision',active_generation_set_id='set' WHERE id='doc';
INSERT INTO binding_definitions VALUES('space','kb','binding','engine',repeat('a',64),'index','{"schema_version":"1.0","generation_id":"generation"}');
INSERT INTO binding_definitions VALUES('space','kb','source-cache','cache-engine',repeat('a',64),'cache','{}');
INSERT INTO binding_definitions VALUES('space','kb','unused-cache','cache-engine',repeat('a',64),'cache','{}');
INSERT INTO binding_definitions VALUES('space','kb','wrong-binding','engine',repeat('a',64),'index','{"generation_id":"different-generation"}');
INSERT INTO publication_cleanup_dependencies VALUES('space','kb','doc','revision','set',1,'source-cache','cache-engine',repeat('a',64),'cache');
CREATE FUNCTION fixture_withdraw(suffix text,fail_before_outbox boolean,omit_outbox boolean DEFAULT false,omit_target boolean DEFAULT false,omit_audit boolean DEFAULT false,omit_dependency boolean DEFAULT false,extra_dependency boolean DEFAULT false,index_binding text DEFAULT 'binding') RETURNS void LANGUAGE plpgsql AS $$
DECLARE current_epoch numeric; affected integer;
BEGIN
 SELECT publication_epoch INTO current_epoch FROM documents WHERE id='doc' FOR UPDATE;
 IF current_epoch<>1 THEN RAISE EXCEPTION 'stale expected epoch' USING ERRCODE='40001'; END IF;
 INSERT INTO command_operations VALUES('op-'||suffix,'space','kb','doc',repeat('b',64),'actor','withdraw',suffix,repeat('c',64),'pending',NULL);
 INSERT INTO publication_history(id,operation_id,space_id,kb_id,document_id,previous_epoch,publication_epoch,action,previous_revision_id,previous_generation_set_id,actor_id,actor,reason,occurred_at,command_digest)
 VALUES('history-'||suffix,'op-'||suffix,'space','kb','doc',1,2,'withdraw','revision','set','actor',
 '{"subject_id":"actor","subject_type":"user","issuer":"test-fixture"}','withdraw','2026-10-14T00:00:00.000000001Z',repeat('c',64));
 INSERT INTO cleanup_plans(id,history_id,operation_id,space_id,kb_id,document_id,revision_id,generation_set_id,publication_epoch,profile_digest,created_at)
 VALUES('plan-'||suffix,'history-'||suffix,'op-'||suffix,'space','kb','doc','revision','set',1,repeat('a',64),'2026-10-14T00:00:00.000000001Z');
 IF NOT omit_target THEN
 INSERT INTO cleanup_targets VALUES('space','kb','doc','revision','set','plan-'||suffix,'target','index','engine',index_binding,repeat('a',64),'generation','epoch','keyword');
 END IF;
 IF NOT omit_dependency THEN
 INSERT INTO cleanup_targets VALUES('space','kb','doc','revision','set','plan-'||suffix,'cache-target','cache','cache-engine','source-cache',repeat('a',64),NULL,NULL,NULL);
 END IF;
 IF extra_dependency THEN
 INSERT INTO cleanup_targets VALUES('space','kb','doc','revision','set','plan-'||suffix,'extra-target','cache','cache-engine','unused-cache',repeat('a',64),NULL,NULL,NULL);
 END IF;
 IF fail_before_outbox THEN RAISE EXCEPTION 'injected failure'; END IF;
 IF NOT omit_outbox THEN
 INSERT INTO withdraw_outbox VALUES('event-'||suffix,'space','kb','doc','plan-'||suffix,'history-'||suffix,'op-'||suffix,'publication.withdrawn',2);
 END IF;
 UPDATE documents SET publication_epoch=2,row_version=row_version+1,active_revision_id=NULL,active_generation_set_id=NULL WHERE id='doc' AND publication_epoch=1 AND row_version=1;
 GET DIAGNOSTICS affected=ROW_COUNT; IF affected<>1 THEN RAISE EXCEPTION 'CAS lost' USING ERRCODE='40001'; END IF;
 IF NOT omit_audit THEN
 INSERT INTO withdraw_audit SELECT 'audit-'||suffix,'request-'||suffix,space_id,kb_id,document_id,id,operation_id,'publication.withdraw',actor_id,actor,occurred_at FROM publication_history WHERE id='history-'||suffix;
 END IF;
 UPDATE command_operations SET state='committed',history_id='history-'||suffix WHERE id='op-'||suffix;
END; $$;
SELECT expect_error('SELECT fixture_withdraw(''rollback'',true)','P0001');
SELECT expect_error('DO $x$ BEGIN PERFORM fixture_withdraw(''no-outbox'',false,true); SET CONSTRAINTS ALL IMMEDIATE; END; $x$','23514');
SELECT expect_error('DO $x$ BEGIN PERFORM fixture_withdraw(''no-target'',false,false,true); SET CONSTRAINTS ALL IMMEDIATE; END; $x$','23514');
SELECT expect_error('DO $x$ BEGIN PERFORM fixture_withdraw(''no-audit'',false,false,false,true); SET CONSTRAINTS ALL IMMEDIATE; END; $x$','23514');
SELECT expect_error('DO $x$ BEGIN PERFORM fixture_withdraw(''missing-source'',false,omit_dependency=>true); SET CONSTRAINTS ALL IMMEDIATE; END; $x$','23514');
SELECT expect_error('DO $x$ BEGIN PERFORM fixture_withdraw(''extra-source'',false,extra_dependency=>true); SET CONSTRAINTS ALL IMMEDIATE; END; $x$','23514');
SELECT expect_error('DO $x$ BEGIN PERFORM fixture_withdraw(''wrong-index'',false,index_binding=>''wrong-binding''); SET CONSTRAINTS ALL IMMEDIATE; END; $x$','23514');
DO $$ BEGIN
 IF (SELECT count(*) FROM publication_history)<>0 OR (SELECT count(*) FROM cleanup_plans)<>0 OR
    (SELECT count(*) FROM cleanup_progress)<>0 OR (SELECT count(*) FROM withdraw_audit)<>0 OR (SELECT count(*) FROM withdraw_outbox)<>0 OR (SELECT count(*) FROM cleanup_targets)<>0 OR
    (SELECT count(*) FROM command_operations)<>0 OR (SELECT publication_epoch FROM documents WHERE id='doc')<>1 THEN
 RAISE EXCEPTION 'partial transaction escaped rollback'; END IF;
END; $$;
BEGIN;
SELECT fixture_withdraw('success',false);
COMMIT;
DO $$ BEGIN
 IF (SELECT publication_epoch FROM documents WHERE id='doc')<>2 OR
    (SELECT active_revision_id FROM documents WHERE id='doc') IS NOT NULL OR
    (SELECT count(*) FROM cleanup_progress)<>2 OR (SELECT count(*) FROM withdraw_outbox)<>1 OR (SELECT count(*) FROM withdraw_audit)<>1 THEN RAISE EXCEPTION 'withdraw commit incomplete'; END IF;
 IF (SELECT occurred_at FROM publication_history WHERE id='history-success')<>'2026-10-14T00:00:00.000000001Z' THEN RAISE EXCEPTION 'nanoseconds lost'; END IF;
END; $$;
SELECT expect_error('SELECT fixture_withdraw(''stale'',false)','40001');
SELECT expect_error('UPDATE withdraw_audit SET request_id=''changed'' WHERE id=''audit-success''','23514');
SELECT expect_error('DELETE FROM withdraw_audit WHERE id=''audit-success''','23514');
SELECT expect_error('INSERT INTO withdraw_audit SELECT ''wrong-actor'',''request'',space_id,kb_id,document_id,id,operation_id,''publication.withdraw'',''other'',actor,occurred_at FROM publication_history WHERE id=''history-success''','23514');
SELECT expect_error('UPDATE publication_history SET reason=''changed'' WHERE id=''history-success''','23514');
SELECT expect_error('UPDATE cleanup_plans SET publication_epoch=2 WHERE id=''plan-success''','23514');
SELECT expect_error('UPDATE binding_definitions SET definition=''{}'' WHERE id=''binding''','23514');
SELECT expect_error('INSERT INTO cleanup_targets VALUES(''space'',''kb'',''doc'',''revision'',''set'',''plan-success'',''missing-channel'',''index'',''engine'',''binding'',repeat(''a'',64),''generation'',''epoch'',NULL)','23514');
INSERT INTO binding_definitions VALUES('other-space','kb','cache-binding','cache-engine',repeat('a',64),'cache','{}');
SELECT expect_error('INSERT INTO cleanup_targets VALUES(''other-space'',''kb'',''doc'',''revision'',''set'',''plan-success'',''foreign'',''cache'',''cache-engine'',''cache-binding'',repeat(''a'',64),NULL,NULL,NULL)','23503');
SELECT expect_error('INSERT INTO cleanup_targets VALUES(''space'',''kb'',''doc'',''revision'',''set'',''plan-success'',''duplicate'',''index'',''engine'',''binding'',repeat(''a'',64),''generation'',''epoch'',''keyword'')','23514');
SELECT expect_error('INSERT INTO cleanup_targets VALUES(''space'',''kb'',''doc'',''revision'',''set'',''plan-success'',''late'',''cache'',''cache-engine'',''unused-cache'',repeat(''a'',64),NULL,NULL,NULL)','23514');
SELECT expect_error('INSERT INTO publication_cleanup_dependencies VALUES(''space'',''kb'',''doc'',''revision'',''set'',1,''unused-cache'',''cache-engine'',repeat(''a'',64),''cache'')','23514');
SELECT expect_error('UPDATE publication_cleanup_dependencies SET publication_epoch=2 WHERE binding_reference=''source-cache''','23514');
DO $$ BEGIN
 IF to_regclass('akos_withdraw_v1.event_consumer_cursors') IS NULL OR to_regclass('akos_withdraw_v1.document_delete_job_control_outbox') IS NULL THEN
  RAISE EXCEPTION 'event persistence tables missing';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='akos_withdraw_v1' AND table_name='document_delete_job_control_outbox' AND column_name='event_seq') THEN
  RAISE EXCEPTION 'event sequence missing';
 END IF;
END; $$;
INSERT INTO event_consumer_cursors(consumer_id,cursor) VALUES('cursor-test','event-b');
INSERT INTO event_consumer_cursors(consumer_id,cursor) VALUES('cursor-test','event-a') ON CONFLICT(consumer_id) DO UPDATE SET cursor=EXCLUDED.cursor WHERE event_consumer_cursors.cursor<=EXCLUDED.cursor;
DO $$ BEGIN
 IF (SELECT cursor FROM event_consumer_cursors WHERE consumer_id='cursor-test')<>'event-b' THEN RAISE EXCEPTION 'cursor regressed'; END IF;
END; $$;
DELETE FROM event_consumer_cursors WHERE consumer_id='cursor-test';SELECT 'PASS: PostgreSQL withdrawal constraint prototype' AS result;

"""Independent logical-deletion evidence; never a physical-purge acceptance."""
import base64
import hashlib
import json
import re
from jsonschema import Draft202012Validator
from pathlib import Path
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def instant(value):
    match = re.fullmatch(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?Z', value)
    if not match:
        raise ValueError('invalid deletion UTC nanosecond timestamp')
    return match[1] + '.' + (match[2] or '').ljust(9, '0') + 'Z'


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or not report.get('sources_unchanged_during_checks'):
        raise ValueError('failed/stale deletion evidence')
    proof = report['go_repository_proof']
    samples = proof.get('document_deletion_samples', [])
    if [s['case'] for s in samples] != ['published', 'unpublished', 'withdrawn']:
        raise ValueError('deletion state matrix missing')
    for sample in samples:
        request, result, head, history = (sample[k] for k in ('request', 'result', 'head', 'history'))
        command = request['Command']
        scope, principal = command['Scope'], command['Principal']
        doc = request['DocumentID']
        if scope['Action'] != 'document.delete' or scope['SubjectID'] != principal['SubjectID'] or history['action'] != 'delete' or sample['governance_authority'] != 'fixture':
            raise ValueError('deletion action/authority differs')
        if (head['space_id'], head['kb_id'], head['id']) != (scope['SpaceID'], scope['KnowledgeBaseIDs'][0], doc) or not head['deleted'] or head['active_revision_id'] is not None or head['active_generation_set_id'] is not None:
            raise ValueError('actual document tombstone/ownership differs')
        if result['DocumentID'] != doc or result['DocumentVersion'] != request['ExpectedDocumentVersion'] + 1 or int(head['row_version']) != result['DocumentVersion'] or int(head['publication_epoch']) != result['PublicationEpoch']:
            raise ValueError('actual deletion document CAS differs')
        if history['document_id'] != doc or history['id'] != result['HistoryID'] or history['operation_id'] != result['OperationID'] or int(history['publication_epoch']) != result['PublicationEpoch'] or int(history['previous_epoch']) + 1 != result['PublicationEpoch'] or history['revision_id'] is not None or history['generation_set_id'] is not None:
            raise ValueError('actual delete history/result differs')
        if instant(history['occurred_at']) != instant(result['DeletedAt']) or history['actor']['subject_id'] != principal['SubjectID'] or history['actor']['issuer'] != principal['Issuer']:
            raise ValueError('delete actor/time binding differs')
        task = sample['job']
        job = task['job']
        if task['id'] != result['JobID'] or task['operation_id'] != result['OperationID'] or task['history_id'] != result['HistoryID'] or task['document_id'] != doc or task['space_id'] != scope['SpaceID'] or task['kb_id'] != scope['KnowledgeBaseIDs'][0] or task['profile_digest'] != command['Request']['ProfileDigest']:
            raise ValueError('actual deletion cleanup job is unbound')
        if job['ID'] != task['id'] or job['SpaceID'] != task['space_id'] or job['OperationID'] != task['operation_id'] or job['ProfileDigest'] != task['profile_digest'] or job['Stage'] != 'document.delete.cleanup' or job['State'] != 'queued' or job['RowVersion'] != 1 or job['Attempt'] != 0 or job['FencingToken'] != 0 or job['LeaseUntil'] is not None or job['Progress'] != 0 or job['Error'] is not None:
            raise ValueError('persisted initial deletion job differs')
        if sample['case'] != 'published':
            if result['CleanupPlanID'] is not None or history['previous_revision_id'] is not None or history['previous_generation_set_id'] is not None:
                raise ValueError('inactive deletion fabricated a source/cleanup')
            if sample['case'] == 'unpublished' and int(history['previous_epoch']) != 0:
                raise ValueError('unpublished deletion has earlier publication')
            if sample['case'] == 'withdrawn':
                previous = sample['previous_history']
                if previous['document_id'] != doc or previous['action'] != 'withdraw' or int(previous['publication_epoch']) != int(history['previous_epoch']):
                    raise ValueError('actual preceding withdrawal missing')
            continue
        receipt, event, audit, plan = (sample[k] for k in ('receipt', 'event', 'audit', 'plan'))
        if receipt['result'] != result or int(receipt['expected_version']) != request['ExpectedDocumentVersion'] or int(receipt['result_version']) != result['DocumentVersion'] or int(receipt['publication_epoch']) != result['PublicationEpoch']:
            raise ValueError('immutable deletion receipt differs')
        for record in (receipt, event, audit):
            if (record['space_id'], record['kb_id'], record['document_id'], record['history_id'], record['operation_id']) != (head['space_id'], head['kb_id'], doc, history['id'], history['operation_id']):
                raise ValueError('deletion delivery cross-resource binding differs')
        if event['id'] != receipt['event_id'] or event['event_type'] != 'document.deleted' or int(event['aggregate_version']) != result['PublicationEpoch'] or audit['id'] != receipt['audit_id'] or audit['action'] != 'document.delete' or audit['actor'] != history['actor'] or instant(audit['occurred_at']) != instant(history['occurred_at']) or audit['request_id'] != command['Request']['RequestID']:
            raise ValueError('actual deletion audit/outbox differs')
        if plan['id'] != result['CleanupPlanID'] or receipt['plan_id'] != plan['id'] or plan['history_action'] != 'delete' or plan['history_id'] != history['id'] or plan['operation_id'] != history['operation_id'] or int(plan['publication_epoch']) != int(history['previous_epoch']) or plan['revision_id'] != history['previous_revision_id'] or plan['generation_set_id'] != history['previous_generation_set_id']:
            raise ValueError('fixed deletion cleanup source differs')
        generations = {g['id']: g for g in sample['generations']}
        targets = sample['targets']
        if len(targets) != sample['cleanup_target_count'] or {t['generation_id'] for t in targets} != set(generations):
            raise ValueError('deletion cleanup index coverage differs')
        for target in targets:
            generation = generations[target['generation_id']]
            for key in ('space_id', 'kb_id', 'document_id', 'revision_id', 'generation_set_id', 'module_instance_id', 'profile_digest', 'backend_epoch_id', 'channel'):
                if target[key] != generation[key]:
                    raise ValueError('deletion target differs from actual generation owner')
            if target['plan_id'] != plan['id'] or target['kind'] != 'index' or target['generation_set_id'] != plan['generation_set_id']:
                raise ValueError('deletion target/plan relation differs')
        original = sample['original']
        blob, raw = original['blob'], base64.b64decode(original['bytes'], validate=True)
        if len(raw) != blob['Size'] or hashlib.sha256(raw).hexdigest() != blob['SHA256'] or original['authority'] != 'independent-original-fixture' or sample['retained_revision_count'] != 3:
            raise ValueError('logical deletion failed original/revision retention')
        if not any(e['original_blob'] == blob and base64.b64decode(e['original_bytes'], validate=True) == raw for e in proof['evidence_backend_samples']):
            raise ValueError('retained original differs from actual pre-delete bytes')
        denials = sample['transport_denials']
        if [d['phase'] for d in denials] != ['deleted-old-cursor', 'deleted-fresh-request']:
            raise ValueError('deleted fresh/cursor transport denial matrix missing')
        for denial in denials:
            mcp = denial['mcp_result']['result']
            if denial['request']['document_id'] != doc or denial['rest_status'] != 503 or denial['mcp_status'] != 200 or not mcp.get('isError') or len(mcp['content']) != 1 or json.loads(mcp['content'][0]['text']) != mcp['structuredContent']:
                raise ValueError('deleted document transport denial differs')
            for error in (denial['rest_result'], mcp['structuredContent']):
                if set(error) != {'code', 'safe_message', 'request_id', 'retryable'} or error['code'] != 'dependency_unavailable':
                    raise ValueError('deleted response leaked content or changed safe error')
        flags = ('stale_cas_rejected', 'event_failure_rolled_back', 'precommit_revocation_rolled_back', 'missing_audit_commit_rejected', 'raw_tombstone_commit_rejected', 'cold_recovery', 'replay_no_duplicate', 'semantic_conflict_rejected', 'current_read_rejected', 'republish_rejected', 'tombstone_undo_rejected')
        if not all(sample.get(k) is True for k in flags):
            raise ValueError('deletion failure/recovery coverage missing')
    management = proof.get('management_delete_samples', [])
    if len(management) != 1:
        raise ValueError('missing actual management deletion HTTP evidence')
    item = management[0]
    calls = item['http_samples']
    expected = [('read-only-denied', 404), ('missing-hidden', 404), ('delete-only-accepted', 202), ('committed-replay', 202), ('version-conflict', 409), ('deleted-new-key-conflict', 409), ('revoked-token', 401), ('revoked-permission', 404)]
    if [(c['case'], c['status']) for c in calls] != expected:
        raise ValueError('management deletion HTTP authorization matrix differs')
    spec = json.loads((ROOT / 'docs/architecture/openapi/management.v1.json').read_text(encoding='utf-8'))
    accepted = Draft202012Validator({'$ref': '#/components/schemas/DeleteAccepted', 'components': spec['components']})
    error = Draft202012Validator({'$ref': '#/components/schemas/Error', 'components': spec['components']})
    receipt, task, head, grant = (item[k] for k in ('receipt', 'job', 'head', 'stored_grant'))
    if grant['action'] != 'kb.delete' or int(grant['version']) != 4 or item['deleter_grant_count'] != 1 or grant['subject_id'] != 'historical-second-reader' or grant['issuer'] != 'fixture' or grant['client_id'] != 'identity-client' or grant['space_id'] != head['space_id'] or grant['kb_id'] != head['kb_id']:
        raise ValueError('delete-only native permission proof differs')
    for call in calls:
        result = call['result']
        if not call['response_request_id']:
            raise ValueError('management tracing header missing')
        if call['status'] == 202:
            accepted.validate(result)
            if result != {'document_id': head['id'], 'job_id': task['id'], 'publication_epoch': int(head['publication_epoch'])}:
                raise ValueError('DeleteAccepted does not reference actual job/head')
        else:
            error.validate(result)
            if set(result) != {'code', 'safe_message', 'request_id', 'retryable'} or result['request_id'] != call['response_request_id']:
                raise ValueError('management deletion safe error/tracing differs')
    if calls[0]['result']['code'] != calls[1]['result']['code'] or calls[0]['result']['safe_message'] != calls[1]['result']['safe_message'] or calls[2]['result'] != calls[3]['result']:
        raise ValueError('management hidden-target or replay behavior differs')
    if item['receipt_count'] != 1 or item['job_count'] != 1 or not head['deleted'] or int(head['row_version']) != 2 or int(head['publication_epoch']) != 1 or head['active_revision_id'] is not None or head['active_generation_set_id'] is not None:
        raise ValueError('management deletion duplicated effects or failed CAS')
    if receipt['document_id'] != head['id'] or receipt['job_id'] != task['id'] or receipt['result']['JobID'] != task['id'] or task['job']['State'] != 'queued' or task['job']['Stage'] != 'document.delete.cleanup' or task['operation_id'] != receipt['operation_id'] or task['history_id'] != receipt['history_id'] or task['profile_digest'] != item['profile_digest'] or task['job']['ProfileDigest'] != item['profile_digest']:
        raise ValueError('management accepted job/receipt/config differs')
    if item['identity'] != 'actual-rs256-postgres-status' or item['authorization'] != 'actual-kb.delete-sealed-acl':
        raise ValueError('management authority provenance differs')
    if source_inventory() != sources:
        raise ValueError('sources changed during deletion review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'actual_samples': len(samples), 'scope': 'PG16 logical deletion of published/unpublished/actually withdrawn documents; atomic actual head/history/receipt/audit/event/cleanup bindings, rollback/current authorization fixture, committed-response recovery, retained original bytes, fresh/old-cursor REST/MCP denials; actual management DELETE/202/queued job with deletion-only sealed ACL, read-only denial, conflict and token/permission revocations; no physical purge, production bootstrap/IAM or full V1 acceptance'}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/document-deletion-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 logical document deletion: actual atomic records, retention and read denials independently verified')

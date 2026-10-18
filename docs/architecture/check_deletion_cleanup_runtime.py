"""Independent fixed deletion-job cleanup, actual effects and live status."""
import base64
import hashlib
import json
import math
from pathlib import Path
from jsonschema import Draft202012Validator
from canonical_content import canonical
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def project(record):
    job = record['Job']
    value = dict(zip(('id', 'space_id', 'resource_id', 'state', 'stage', 'attempt', 'row_version', 'progress'), (job['ID'], job['SpaceID'], record['ResourceID'], job['State'], job['Stage'], job['Attempt'], job['RowVersion'], job['Progress'])))
    if job['Error'] is not None:
        error = job['Error']
        value['error'] = {'code': error['Code'], 'safe_message': error['SafeMessage'], 'retryable': error['Retryable'], 'request_id': error['RequestID']}
    return value


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or not report.get('sources_unchanged_during_checks'):
        raise ValueError('cleanup report is stale/failed')
    proof = report['go_repository_proof']
    samples = proof.get('deletion_cleanup_samples', [])
    if len(samples) != 1:
        raise ValueError('actual deletion cleanup sample missing')
    sample = samples[0]
    spec = json.loads((ROOT / 'docs/architecture/openapi/management.v1.json').read_text(encoding='utf-8'))
    validator = Draft202012Validator({'$ref': '#/components/schemas/Job', 'components': spec['components']})
    deletion = proof['document_deletion_samples'][0]
    job_id, doc, total = sample['job_id'], sample['resource_id'], sample['target_count']
    if job_id != deletion['result']['JobID'] or doc != deletion['result']['DocumentID'] or total < 2 or len(sample['steps']) != total:
        raise ValueError('cleanup job/retired source binding differs')
    before = sample['before']
    validator.validate(before)
    if before['id'] != job_id or before['resource_id'] != doc or before['state'] != 'queued' or before['row_version'] != 1 or before['progress'] != 0:
        raise ValueError('cleanup initial status differs')
    for index, step in enumerate(sample['steps']):
        record, wire = step['record'], step['http_result']
        validator.validate(wire)
        job = record['Job']
        if wire != project(record) or record['ResourceID'] != doc or job['ID'] != job_id or job['OperationID'] != deletion['result']['OperationID'] or job['ProfileDigest'] != deletion['request']['Command']['Request']['ProfileDigest'] or job['Attempt'] != 1 or job['RowVersion'] != index + 3 or not math.isclose(job['Progress'], (index + 1) / total, abs_tol=1e-12) or job['State'] != ('completed' if index + 1 == total else 'running'):
            raise ValueError('actual cleanup progression/HTTP projection differs')
    if sample['completed'] != sample['steps'][-1]['record'] or sample['replay'] != sample['completed']:
        raise ValueError('completed cleanup replay changed state')
    runtime = sample['runtime']
    if runtime['job_id'] != job_id or runtime['state'] != 'completed' or int(runtime['attempt']) != 1 or int(runtime['row_version']) != total + 2 or int(runtime['completed_targets']) != total or int(runtime['total_targets']) != total or runtime['error_request_id'] is not None:
        raise ValueError('persisted aggregate progress differs')
    history = proof['historical_lifecycle_samples'][0]['history'] + [deletion['history']]
    retired = {h['id']: h for h in history if h['previous_revision_id'] is not None}
    actual = sample['actual_targets']
    if len(actual) != total or {row['plan']['history_id'] for row in actual} != set(retired):
        raise ValueError('cleanup missed historical/current retirement plans')
    for row in actual:
        member, target, plan, progress, generation = (row[k] for k in ('membership', 'target', 'plan', 'progress', 'generation'))
        h = retired[plan['history_id']]
        if member['job_id'] != job_id or member['plan_id'] != plan['id'] or member['target_id'] != target['id'] or target['plan_id'] != plan['id'] or progress['plan_id'] != plan['id'] or progress['target_id'] != target['id']:
            raise ValueError('fixed cleanup member relation differs')
        if plan['document_id'] != doc or plan['revision_id'] != h['previous_revision_id'] or plan['generation_set_id'] != h['previous_generation_set_id'] or int(plan['publication_epoch']) != int(h['previous_epoch']) or plan['operation_id'] != h['operation_id'] or plan['history_action'] != h['action']:
            raise ValueError('fixed retired publication identity differs')
        for key in ('space_id', 'kb_id', 'document_id', 'revision_id', 'generation_set_id', 'module_instance_id', 'profile_digest', 'backend_epoch_id', 'channel'):
            if target[key] != generation[key]:
                raise ValueError('actual generation owner/config differs')
        if target['generation_id'] != generation['id'] or target['kind'] != 'index' or row['deleted'] is not True or row['remaining_rows'] != 0:
            raise ValueError('actual native index deletion not proven')
        body = {'format': 'akos.cleanup.effect/1', 'space_id': plan['space_id'], 'knowledge_base_id': plan['kb_id'], 'document_id': doc, 'plan_id': plan['id'], 'target_id': target['id'], 'revision_id': plan['revision_id'], 'generation_set_id': plan['generation_set_id'], 'publication_epoch': str(int(plan['publication_epoch'])), 'kind': target['kind'], 'module_instance_id': target['module_instance_id'], 'binding_reference': target['binding_reference'], 'profile_digest': target['profile_digest'], 'generation_id': target['generation_id'], 'backend_epoch_id': target['backend_epoch_id'], 'channel': target['channel']}
        digest = hashlib.sha256(b'akos.cleanup.effect/1\n' + canonical(json.dumps(body, ensure_ascii=False).encode('utf-8'))).hexdigest()
        if progress['state'] != 'succeeded' or int(progress['attempt']) != 1 or int(progress['fencing_token']) != 1 or int(progress['row_version']) != 3 or progress['lease_until'] is not None or progress['result_reference'] != 'cleanup-' + digest + '-deleted':
            raise ValueError('verified per-target receipt/lease progression differs')
    empty = sample['empty_record']
    if sample['empty_target_count'] != 0 or empty['Job']['ID'] != proof['management_delete_samples'][0]['job']['id'] or empty['Job']['State'] != 'completed' or empty['Job']['Progress'] != 1 or empty['Job']['RowVersion'] != 3 or sample['empty_http'] != project(empty):
        raise ValueError('no-work cleanup job completion differs')
    validator.validate(sample['empty_http'])
    failed, failed_runtime = sample['failed_record'], sample['failed_runtime']
    failed_tasks = sample['failed_targets']
    if sample['failed_replay'] != failed or sample['failed_http'] != project(failed) or failed['Job']['State'] != 'failed' or failed_runtime['state'] != 'failed' or failed_runtime['job_id'] != failed['Job']['ID'] or not any(t['state'] == 'unknown' for t in failed_tasks) or int(failed_runtime['completed_targets']) != sum(t['state'] == 'succeeded' for t in failed_tasks) or int(failed_runtime['total_targets']) != len(failed_tasks):
        raise ValueError('unknown effect was retried or falsely completed')
    error = failed['Job']['Error']
    if error != {'Code': 'dependency_unavailable', 'SafeMessage': 'Cleanup requires reconciliation', 'Retryable': False, 'RequestID': 'delete-job-backend-unavailable'} or failed_runtime['error_request_id'] != error['RequestID']:
        raise ValueError('failed cleanup safe error/trace differs')
    validator.validate(sample['failed_http'])
    failed_index = sample['failed_index']
    if failed_index['document_id'] != failed['ResourceID'] or failed_index['deleted'] or failed_index['remaining_rows'] != 1 or sample['failure_source_authority'] != 'fixture-metadata-and-governance':
        raise ValueError('unavailable backend falsely removed fixture index')

    original = sample['original']
    raw = base64.b64decode(original['bytes'], validate=True)
    if original != deletion['original'] or len(raw) != original['blob']['Size'] or hashlib.sha256(raw).hexdigest() != original['blob']['SHA256']:
        raise ValueError('cleanup changed retained original bytes')
    untouched = sample['untouched_before']
    if untouched != sample['untouched_after'] or untouched['document_id'] == doc or untouched['deleted'] or untouched['remaining_rows'] <= 0:
        raise ValueError('cleanup affected another document source')
    grant = sample['status_grant']
    who = proof['identity_token_samples'][0]['principal']
    if int(grant['version']) != 8 or grant['action'] != 'resource.view_job' or grant['subject_id'] != who['SubjectID'] or grant['issuer'] != who['Issuer'] or grant['client_id'] != who['ClientID'] or grant['space_id'] != deletion['head']['space_id'] or grant['kb_id'] != deletion['head']['kb_id']:
        raise ValueError('live progress GET permission differs')
    if not all(sample[k] is True for k in ('forged_success_rejected', 'raw_completion_rejected', 'sealed_targets_rejected')) or sample['worker_authority'] != 'fixture-independent-task-and-index-grants' or sample['status_authority'] != 'actual-rs256-resource.view_job':
        raise ValueError('cleanup guard/authority provenance missing')
    if source_inventory() != sources:
        raise ValueError('cleanup sources changed during review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'actual_targets': total, 'scope': 'PG16 bounded deletion-job consumer over all fixed retired publication targets, real per-target leases/native index fences/zero rows/receipt hashes, aggregate HTTP running/completed and failed unknown-effect isolation, terminal replay/no-work completion, unrelated generation and original retained; task/index grants and resolver failure fixture, real status authentication/ACL; not continuous queue, other backends, purge or full V1 acceptance'}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/deletion-cleanup-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 deletion cleanup: all retired targets, native effects, leases, receipts and live status independently verified')

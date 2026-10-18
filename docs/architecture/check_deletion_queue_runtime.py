"""Independent queue discovery, cursor binding and real deletion effects."""
import hashlib
import json
from pathlib import Path

from canonical_content import canonical
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or report.get('sources_unchanged_during_checks') is not True:
        raise ValueError('queue report stale/failed')
    samples = report['go_repository_proof'].get('deletion_queue_samples', [])
    if len(samples) != 1:
        raise ValueError('queue sample missing')
    sample = samples[0]
    seeds, pages, cycles = (sample[k] for k in ('seeds', 'pages', 'cycles'))
    if len(seeds) != 4 or len(pages) != 3 or len(cycles) != 3 or sample['scope_renewals'] != 3:
        raise ValueError('queue did not renew authority/process bounded cycles')
    by_job = {s['JobID']: s for s in seeds}
    active = sorted(s['JobID'] for s in seeds[:3])
    failed_id = seeds[3]['JobID']
    definitions = {r['definition']['id']: r for r in sample['runtime']}
    if set(definitions) != set(by_job):
        raise ValueError('queue lost/created job definitions')
    previous_scope = None
    for index, row in enumerate(pages):
        request, page = row['request'], row['page']
        scope = request['Scope']
        if request['Limit'] != 1 or request['WorkerID'] != 'worker-a' or scope['SubjectID'] != request['WorkerID'] or scope['SpaceID'] != 'space' or scope['KnowledgeBaseIDs'] != ['delete-queue-kb'] or scope['Action'] != 'publication.cleanup.execute' or scope['PolicyVersion'] != (1 if index == 0 else 2) or scope['ScopeID'] == previous_scope:
            raise ValueError('queue authority namespace/renewal differs')
        previous_scope = scope['ScopeID']
        if len(page['Items']) != 1 or page['Items'][0]['JobID'] != active[index] or page['UpperJobID'] != active[-1]:
            raise ValueError('queue page bound/order/highwater differs')
        ticket = page['Items'][0]
        definition = definitions[ticket['JobID']]['definition']
        if ticket['ResourceID'] != by_job[ticket['JobID']]['DocumentID'] or ticket['SpaceID'] != definition['space_id'] or ticket['KnowledgeBaseID'] != definition['kb_id'] or ticket['ProfileDigest'] != definition['profile_digest']:
            raise ValueError('queue ticket definition/config binding differs')
        body = {'format': 'akos.delete.queue-cursor/1', 'subject': scope['SubjectID'], 'worker': request['WorkerID'], 'space': scope['SpaceID'], 'kb': scope['KnowledgeBaseIDs'][0], 'action': scope['Action'], 'policy': str(scope['PolicyVersion']), 'limit': request['Limit']}
        digest = hashlib.sha256(b'akos.delete.queue-cursor/1\n' + canonical(json.dumps(body).encode('utf-8'))).hexdigest()
        expected_next = {'AfterJobID': active[index], 'UpperJobID': active[-1], 'Binding': digest} if index < 2 else None
        if page['Next'] != expected_next:
            raise ValueError('queue cursor canonical binding differs')
        expected_cursor = None if index < 2 else pages[index - 1]['page']['Next']
        if request['Cursor'] != expected_cursor:
            raise ValueError('policy reset or stable sweep continuation differs')
        if cycles[index] != {'Discovered': 1, 'Attempted': 1, 'Completed': 1, 'Failed': 0, 'WorkErrors': 0, 'SweepCompleted': index == 2}:
            raise ValueError('poller per-cycle budget/results differ')
    for job_id, row in definitions.items():
        d, p = row['definition'], row['progress']
        seed = by_job[job_id]
        if d['space_id'] != 'space' or d['kb_id'] != 'delete-queue-kb' or d['document_id'] != seed['DocumentID'] or d['operation_id'] != seed['OperationID'] or d['history_id'] != seed['HistoryID'] or p['job_id'] != job_id or int(p['attempt']) != 1 or int(p['row_version']) != 3:
            raise ValueError('queue durable definition/progress differs')
        if p['state'] != ('failed' if job_id == failed_id else 'completed'):
            raise ValueError('queue terminal states differ')
        expected_total = 1 if seed in seeds[2:] else 0
        if int(p['total_targets']) != expected_total or int(p['completed_targets']) != (expected_total if job_id != failed_id else 0):
            raise ValueError('queue counts do not match physical targets')
    if len(sample['physical']) != 2:
        raise ValueError('queue physical targets missing')
    for row in sample['physical']:
        m, t, p, g = (row[k] for k in ('membership', 'target', 'progress', 'generation'))
        job_id = m['job_id']
        failed = job_id == failed_id
        if job_id not in (seeds[2]['JobID'], failed_id) or m['plan_id'] != t['plan_id'] or m['target_id'] != t['id'] or p['plan_id'] != m['plan_id'] or p['target_id'] != m['target_id'] or g['id'] != t['generation_id'] or g['document_id'] != by_job[job_id]['DocumentID'] or g['kb_id'] != 'delete-queue-kb':
            raise ValueError('queue target/generation identity differs')
        if row['deleted'] is not (not failed) or row['remaining_rows'] != (1 if failed else 0) or p['state'] != ('unknown' if failed else 'succeeded') or int(p['attempt']) != 1 or int(p['fencing_token']) != 1 or int(p['row_version']) != 3 or p['lease_until'] is not None:
            raise ValueError('queue actual effects/failed no retry differs')
    if sample['failed'] != sample['failed_replay'] or sample['failed']['Job']['ID'] != failed_id or sample['failed']['Job']['State'] != 'failed' or sample['failed']['Job']['Error']['Retryable'] is not False:
        raise ValueError('queue retried unknown effect')
    if sample['terminal_discovery']['Items'] != [] or sample['terminal_discovery']['Next'] is not None or sample['other_before'] != sample['other_after'] or not sample['other_before']:
        raise ValueError('queue terminal exclusion/other-KB isolation differs')
    for key in ('stale_policy_cursor_denied', 'cross_kb_denied', 'expired_scope_denied', 'revoked_scope_denied', 'canceled'):
        if sample[key] is not True:
            raise ValueError('queue guard/cancellation missing: ' + key)
    if sample['source_authority'] != 'fixture-metadata-and-governance' or sample['worker_authority'] != 'fixture-independent-task-and-index-grants':
        raise ValueError('queue authority provenance missing')
    if source_inventory() != sources:
        raise ValueError('queue sources changed during review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'completed_jobs': 3, 'isolated_failed_jobs': 1, 'scope': 'PG16 scoped keyset discovery, canonical internal cursor binding/policy reset, bounded fair polling and cancellation, actual native index fence/zero rows and no-target completion, failed unknown excluded/no retry, other KB unchanged; metadata and task/index grants are fixtures, not production IAM/bootstrap or durable scheduler/reconciliation/purge/full V1 acceptance'}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/deletion-queue-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 deletion queue: scoped pages, cursor hashes, policy reset, physical effects and cancellation independently verified')

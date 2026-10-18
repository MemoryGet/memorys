"""Independent service identity, current ACL and native cleanup admission proof."""
import json
from pathlib import Path

from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or report.get('sources_unchanged_during_checks') is not True:
        raise ValueError('worker authority report stale/failed')
    samples = report['go_repository_proof'].get('worker_authority_samples', [])
    if len(samples) != 1:
        raise ValueError('worker authority sample missing')
    sample = samples[0]
    identity = sample['identity']
    issuer, subject, client, key, token = (identity[k] for k in ('issuer', 'subject', 'client', 'key', 'token'))
    if issuer['issuer'] != 'worker-issuer' or not issuer['enabled'] or subject['subject_id'] != 'cleanup-service' or subject['subject_type'] != 'service' or not subject['enabled'] or client['client_id'] != 'cleanup-client' or not client['enabled'] or key['key_id'] != 'worker-key' or not key['enabled'] or token['token_id'] != 'cleanup-token' or token['revoked'] or token['subject_id'] != subject['subject_id'] or token['client_id'] != client['client_id'] or token['key_id'] != key['key_id']:
        raise ValueError('native service identity registry differs')
    if any(row['issuer'] != issuer['issuer'] for row in (subject, client, key, token)) or len(key['public_key_digest']) != 64 or not int(key['valid_from']) <= int(token['issued_at']) < int(token['expires_at']) <= int(key['valid_until']):
        raise ValueError('native worker identity key/lifetime binding differs')
    expected_grants = {9: {'publication.cleanup.execute'}, 10: {'publication.cleanup.execute', 'index.delete'}, 11: {'index.delete'}, 12: {'publication.cleanup.execute', 'index.delete'}}
    actual_grants = {v: set() for v in expected_grants}
    for grant in sample['grants']:
        version = int(grant['version'])
        if version not in actual_grants or grant['issuer'] != issuer['issuer'] or grant['subject_id'] != subject['subject_id'] or grant['client_id'] != client['client_id'] or grant['space_id'] != 'space' or grant['kb_id'] != 'delete-queue-kb':
            raise ValueError('native worker ACL identity/resource differs')
        actual_grants[version].add(grant['action'])
    if actual_grants != expected_grants or len(sample['grants']) != sum(map(len, expected_grants.values())):
        raise ValueError('native independent permissions/policy transitions differ')
    if {int(s['version']) for s in sample['policy_snapshots']} != set(expected_grants) or any(not s['sealed'] or s['space_id'] != 'space' for s in sample['policy_snapshots']):
        raise ValueError('worker ACL snapshots were not sealed')
    for version in (9, 10, 12):
        s = sample['scope' + str(version)]
        if s['SubjectID'] != subject['subject_id'] or s['Action'] != 'publication.cleanup.execute' or s['SpaceID'] != 'space' or s['KnowledgeBaseIDs'] != ['delete-queue-kb'] or s['PolicyVersion'] != version:
            raise ValueError('worker current scope binding differs')
    decisions = sample['decisions']
    if sample['successful_authentications'] <= len(decisions) or len(decisions) < 10:
        raise ValueError('worker did not repeatedly authenticate/current-authorize')
    index_decisions = []
    for decision in decisions:
        q, s = decision['request'], decision['scope']
        who = q['Principal']
        if who != {'SubjectID': subject['subject_id'], 'SubjectType': 'service', 'Issuer': issuer['issuer'], 'ClientID': client['client_id'], 'Delegation': None} or q['SpaceID'] != 'space' or q['KnowledgeBaseIDs'] != ['delete-queue-kb'] or q['Action'] not in actual_grants[s['PolicyVersion']] or s['SubjectID'] != subject['subject_id'] or s['Action'] != q['Action'] or s['SpaceID'] != q['SpaceID'] or s['KnowledgeBaseIDs'] != q['KnowledgeBaseIDs']:
            raise ValueError('worker decision not backed by actual sealed grant')
        if q['Action'] == 'index.delete':
            index_decisions.append(decision)
            if s['PolicyVersion'] != 12 or q['ResourceIDs'] != [sample['positive']['DocumentID']]:
                raise ValueError('index grant not independent/current/resource-bound')
    if not index_decisions or len({d['scope']['ScopeID'] for d in decisions}) != len(decisions):
        raise ValueError('worker fabricated/reused decision identity')
    seeds = {sample[k]['JobID']: sample[k] for k in ('positive', 'denied')}
    positive_id, denied_id = sample['positive']['JobID'], sample['denied']['JobID']
    if len(sample['physical']) != 2 or {r['definition']['id'] for r in sample['physical']} != set(seeds):
        raise ValueError('worker physical jobs missing')
    for row in sample['physical']:
        d, p, m, t, task, g = (row[k] for k in ('definition', 'runtime', 'member', 'target', 'task', 'generation'))
        seed = seeds[d['id']]
        failed = d['id'] == denied_id
        if d['document_id'] != seed['DocumentID'] or d['operation_id'] != seed['OperationID'] or d['history_id'] != seed['HistoryID'] or d['kb_id'] != 'delete-queue-kb' or p['job_id'] != d['id'] or m['job_id'] != d['id'] or m['plan_id'] != t['plan_id'] or m['target_id'] != t['id'] or task['plan_id'] != m['plan_id'] or task['target_id'] != t['id'] or t['generation_id'] != g['id'] or g['document_id'] != d['document_id'] or g['kb_id'] != d['kb_id']:
            raise ValueError('worker immutable source/target binding differs')
        if p['state'] != ('failed' if failed else 'completed') or int(p['attempt']) != 1 or int(p['row_version']) != 3 or int(p['total_targets']) != 1 or int(p['completed_targets']) != (0 if failed else 1) or task['state'] != ('unknown' if failed else 'succeeded') or int(task['attempt']) != 1 or int(task['fencing_token']) != 1 or int(task['row_version']) != 3 or task['worker_id'] != subject['subject_id'] or task['lease_until'] is not None or row['deleted'] is not (not failed) or row['remaining_rows'] != (1 if failed else 0):
            raise ValueError('worker real effect/lease/no-retry differs')
    if sample['failed'] != sample['failed_replay'] or sample['failed']['Job']['ID'] != denied_id or sample['failed']['Job']['Error']['Retryable'] is not False:
        raise ValueError('worker retried permission-denied unknown effect')
    if sample['cycle'] != {'Discovered': 1, 'Attempted': 1, 'Completed': 1, 'Failed': 0, 'WorkErrors': 0, 'SweepCompleted': True} or sample['terminal']['Items'] != []:
        raise ValueError('authenticated queue did not complete/exclude terminals')
    if len(sample['operations']) != 2 or {op['DocumentID'] for op in sample['operations']} != {s['DocumentID'] for s in seeds.values()}:
        raise ValueError('worker operation admission sample missing')
    for guard in ('token_client_key_revocation_denied', 'revoked_during_discovery_denied', 'stale_policy_denied', 'relabel_denied', 'finished_operation_denied', 'cross_kb_denied', 'closed_denied'):
        if sample[guard] is not True:
            raise ValueError('worker guard missing: ' + guard)
    if sample['worker_authority'] != 'actual-rs256-service-and-sealed-pg-grants' or sample['source_authority'] != 'fixture-metadata-and-governance' or sample['module_resolution'] != 'in-process-pg-index-adapter-no-production-registry':
        raise ValueError('worker provenance differs')
    if source_inventory() != sources:
        raise ValueError('worker sources changed during review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'scope': 'PG16 actual RS256 service identity and current registry/ACL, independent execution/index resource decisions, lease/member/source operation admission, native index effect and denied unknown no retry, policy/token/client/key revocation and mid-discovery denial, closed authority; source metadata and governance fixture, no production registry/bootstrap/remote Grant/full IAM or V1 acceptance'}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/worker-authority-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 worker authority: actual service identity, independent grants, lease admission and native effects independently verified')

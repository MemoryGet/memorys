"""Independent management job status/authorization proof, not queue acceptance."""
import json
from pathlib import Path
from jsonschema import Draft202012Validator
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or not report.get('sources_unchanged_during_checks'):
        raise ValueError('job status report is failed or stale')
    proof = report['go_repository_proof']
    samples = proof.get('job_status_samples', [])
    if len(samples) != 1:
        raise ValueError('actual job status sample missing')
    sample = samples[0]
    spec = json.loads((ROOT / 'docs/architecture/openapi/management.v1.json').read_text(encoding='utf-8'))
    job_schema = Draft202012Validator({'$ref': '#/components/schemas/Job', 'components': spec['components']})
    error_schema = Draft202012Validator({'$ref': '#/components/schemas/Error', 'components': spec['components']})
    calls = sample['http_samples']
    expected = [('viewer-only-visible', 200), ('delete-only-denied', 404), ('missing-hidden', 404), ('foreign-kb-hidden', 404), ('revoked-token', 401), ('token-revoked-during-read', 401), ('permission-revoked-during-read', 404), ('revoked-permission', 404)]
    if [(c['case'], c['status']) for c in calls] != expected:
        raise ValueError('job status permission/revocation matrix differs')
    stored, foreign = sample['job'], sample['foreign_job']
    job = stored['job']
    management = proof['management_delete_samples'][0]
    if stored != management['job'] or not management['head']['deleted'] or stored['document_id'] != management['head']['id'] or stored['id'] != management['receipt']['job_id']:
        raise ValueError('job status is not the actual deletion task on a deleted resource')
    projected = {'id': stored['id'], 'space_id': stored['space_id'], 'resource_id': stored['document_id'], 'state': job['State'], 'stage': job['Stage'], 'attempt': job['Attempt'], 'row_version': job['RowVersion'], 'progress': job['Progress']}
    if job['ID'] != stored['id'] or job['OperationID'] != stored['operation_id'] or job['SpaceID'] != stored['space_id'] or job['ProfileDigest'] != stored['profile_digest'] or job['State'] != 'queued' or job['Stage'] != 'document.delete.cleanup' or job['RowVersion'] != 1 or job['Progress'] != 0:
        raise ValueError('persisted job initial state/binding differs')
    job_schema.validate(projected)
    if calls[0]['result'] != projected or calls[0]['job_id'] != stored['id']:
        raise ValueError('public job result exposes different/internal fields')
    if foreign['id'] == stored['id'] or foreign['document_id'] == stored['document_id'] or foreign['space_id'] != stored['space_id'] or foreign['kb_id'] == stored['kb_id'] or calls[3]['job_id'] != foreign['id'] or foreign['job']['ID'] != foreign['id'] or foreign['job']['State'] != 'queued':
        raise ValueError('actual foreign-KB task proof missing')
    grants = sample['stored_grants']
    viewer = proof['identity_token_samples'][0]['principal']
    deleter = management['stored_grant']
    if len(grants) != 2 or {(g['subject_id'], g['action']) for g in grants} != {(viewer['SubjectID'], 'resource.view_job'), (deleter['subject_id'], 'kb.delete')}:
        raise ValueError('job-view-only/delete-only separation missing')
    for grant in grants:
        if int(grant['version']) != 6 or grant['space_id'] != stored['space_id'] or grant['kb_id'] != stored['kb_id'] or grant['issuer'] != viewer['Issuer'] or grant['client_id'] != viewer['ClientID']:
            raise ValueError('actual job permission owner/issuer/client differs')
    hidden = None
    for call in calls:
        if not call['response_request_id']:
            raise ValueError('job response tracing header missing')
        if call['status'] == 200:
            job_schema.validate(call['result'])
            continue
        value = call['result']
        error_schema.validate(value)
        if set(value) != {'code', 'safe_message', 'request_id', 'retryable'} or value['request_id'] != call['response_request_id'] or value['retryable'] is not False:
            raise ValueError('job safe error leaked status or changed request binding')
        if call['status'] == 404:
            pair = (value['code'], value['safe_message'])
            if hidden is None:
                hidden = pair
            if pair != hidden or value['code'] != 'not_found':
                raise ValueError('missing/forbidden/foreign/revoked job errors differ')
        elif value['code'] != 'unauthenticated':
            raise ValueError('job token revocation classification differs')
    if sample['owner_mutation_rejected'] is not True or sample['identity'] != 'actual-rs256-postgres-status' or sample['authorization'] != 'actual-resource.view_job-sealed-acl' or sample['foreign_creation_authority'] != 'fixture':
        raise ValueError('job ownership/authority provenance missing')
    if source_inventory() != sources:
        raise ValueError('job status sources changed during review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'actual_samples': len(samples), 'scope': 'PG16 real queued deletion-job GET, management Schema/exact public projection, actual deleted resource and job/receipt binding, job-view-only grant, cross-KB/unknown/delete-only hiding, before/during-read token and permission revocation; foreign job creation fixture; not full TaskRepository, queue consumption, production bootstrap or V1 acceptance'}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/job-status-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 job status: actual task/public Schema/resource permissions/revocations independently verified')

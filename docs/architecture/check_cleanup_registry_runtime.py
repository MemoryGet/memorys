"""Independent sealed cleanup deployment selection and actual named module effects."""
import hashlib
import json
from pathlib import Path

from canonical_content import canonical
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def operation_digest(op):
    t = op['Target']
    body = {'format': 'akos.cleanup.effect/1', 'space_id': op['SpaceID'], 'knowledge_base_id': op['KnowledgeBaseID'], 'document_id': op['DocumentID'], 'plan_id': op['PlanID'], 'target_id': op['TargetID'], 'revision_id': op['RevisionID'], 'generation_set_id': op['GenerationSetID'], 'publication_epoch': str(op['PublicationEpoch']), 'kind': t['Kind'], 'module_instance_id': t['ModuleInstanceID'], 'binding_reference': t['BindingReference'], 'profile_digest': t['ProfileDigest'], 'generation_id': t['GenerationID'], 'backend_epoch_id': t['BackendEpochID'], 'channel': t['Channel']}
    digest = hashlib.sha256(b'akos.cleanup.effect/1\n' + canonical(json.dumps(body).encode('utf-8'))).hexdigest()
    if op['Digest'] != digest or op['ID'] != 'cleanup-' + digest:
        raise ValueError('registered operation canonical identity differs')
    return digest


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or report.get('sources_unchanged_during_checks') is not True:
        raise ValueError('cleanup registry report stale/failed')
    samples = report['go_repository_proof'].get('cleanup_registry_samples', [])
    if len(samples) != 1:
        raise ValueError('cleanup registry sample missing')
    sample = samples[0]
    plan, identity = sample['host_plan'], sample['runtime_identity']
    if plan['bindings'] != {'index_cleanup': 'registry-revoked-engine'} or [m['id'] for m in plan['modules']] != ['registry-positive-engine', 'registry-revoked-engine'] or any(m['provider'] != 'reference.cleanup.postgres' or m['implementation_version'] != '1.0.0' or m['settings'] != {} for m in plan['modules']):
        raise ValueError('independent named host configuration differs')
    # These controlled host modules already carry resolved implementation pins;
    # their order is the same sorted order used by the kernel snapshot format.
    encoded = json.dumps({'Plan': plan, 'Modules': identity['Modules']}, ensure_ascii=False, separators=(',', ':')).encode('utf-8')
    digest = hashlib.sha256(encoded).hexdigest()
    if digest != identity['ConfigVersion'] or identity['Bindings'] != plan['bindings'] or {m['ID'] for m in identity['Modules']} != {m['id'] for m in plan['modules']}:
        raise ValueError('actual kernel config digest/named inventory differs')
    required = {'generation.delete', 'generation.inspect', 'delete.fence', 'fence.durable'}
    for m in identity['Modules']:
        if m['Provider'] != 'reference.cleanup.postgres' or m['ImplementationVersion'] != '1.0.0' or m['CapabilityVersions']['index_cleanup'] != '1.0' or not required <= set(m['CapabilityFeatures']['index_cleanup']):
            raise ValueError('registered actual port capability/implementation differs')
    snapshots = {int(s['version']): s for s in sample['snapshots']}
    if set(snapshots) != set(range(1, 8)) or any(s['space_id'] != 'space' or s['kb_id'] != 'delete-queue-kb' or s['sealed'] is not (v < 7) for v, s in snapshots.items()) or sample['head'] != {'space_id': 'space', 'kb_id': 'delete-queue-kb', 'version': 6}:
        raise ValueError('current sealed deployment activation differs')
    bindings = {(int(b['version']), b['binding_reference']): b for b in sample['bindings']}
    if len(bindings) != 10 or {v for v, _ in bindings} != {1, 3, 4, 5, 6}:
        raise ValueError('retained deployment bindings differ')
    for (v, reference), b in bindings.items():
        prefix = reference.removesuffix('-binding')
        if prefix not in ('registry-positive', 'registry-revoked') or b['space_id'] != 'space' or b['kb_id'] != 'delete-queue-kb' or b['module_instance_id'] != prefix + '-engine' or b['generation_id'] != prefix + '-generation' or b['backend_epoch_id'] != prefix + '-epoch' or b['channel'] != 'keyword' or b['provider'] != 'reference.cleanup.postgres' or b['contract_version'] != '1.0' or b['implementation_version'] != ('2.0.0' if v == 3 else '1.0.0') or b['runtime_config_digest'] != ('f' * 64 if v == 4 else digest) or b['profile_digest'] != ('b' * 64 if v == 5 else 'a' * 64):
            raise ValueError('current deployment mismatch fixtures differ')
    reads = sample['reads']
    if not reads or {r['binding']['Version'] for r in reads} != {1, 3, 4, 5, 6}:
        raise ValueError('per-call deployment resolution evidence missing')
    operations = {}
    fields = {'Version': 'version', 'SpaceID': 'space_id', 'KnowledgeBaseID': 'kb_id', 'BindingReference': 'binding_reference', 'ModuleInstanceID': 'module_instance_id', 'ProfileDigest': 'profile_digest', 'RuntimeConfigDigest': 'runtime_config_digest', 'Provider': 'provider', 'ImplementationVersion': 'implementation_version', 'ContractVersion': 'contract_version', 'GenerationID': 'generation_id', 'BackendEpochID': 'backend_epoch_id', 'Channel': 'channel'}
    for read in reads:
        scope, op, b = read['scope'], read['operation'], read['binding']
        actual = bindings[b['Version'], b['BindingReference']]
        if b != {k: actual[v] for k, v in fields.items()} or scope['SubjectID'] != 'cleanup-service' or scope['Action'] != 'publication.cleanup.execute' or scope['SpaceID'] != 'space' or scope['KnowledgeBaseIDs'] != ['delete-queue-kb'] or scope['PolicyVersion'] != 12:
            raise ValueError('resolved deployment source/current worker authority differs')
        operation_digest(op)
        operations[op['DocumentID']] = op
    seeds = {sample[k]['JobID']: sample[k] for k in ('positive', 'revoked')}
    if len(sample['physical']) != 2 or {row['job']['id'] for row in sample['physical']} != set(seeds):
        raise ValueError('registered consumer actual job targets missing')
    for row in sample['physical']:
        j, p, m, t, task, g = (row[k] for k in ('job', 'runtime', 'member', 'target', 'task', 'generation'))
        seed = seeds[j['id']]
        failed = j['id'] == sample['revoked']['JobID']
        op = operations[seed['DocumentID']]
        if j['document_id'] != seed['DocumentID'] or j['operation_id'] != seed['OperationID'] or j['history_id'] != seed['HistoryID'] or m['job_id'] != j['id'] or p['job_id'] != j['id'] or m['plan_id'] != t['plan_id'] or m['target_id'] != t['id'] or task['plan_id'] != m['plan_id'] or task['target_id'] != m['target_id'] or t['module_instance_id'] != op['Target']['ModuleInstanceID'] or t['profile_digest'] != op['Target']['ProfileDigest'] or t['binding_reference'] != op['Target']['BindingReference'] or op['PlanID'] != t['plan_id'] or op['TargetID'] != t['id'] or t['generation_id'] != g['id'] or g['document_id'] != j['document_id'] or g['kb_id'] != 'delete-queue-kb':
            raise ValueError('registered source/module/target relations differ')
        if p['state'] != ('failed' if failed else 'completed') or int(p['attempt']) != 1 or int(p['row_version']) != 3 or int(p['total_targets']) != 1 or int(p['completed_targets']) != (0 if failed else 1) or task['state'] != ('unknown' if failed else 'succeeded') or task['worker_id'] != 'cleanup-service' or int(task['attempt']) != 1 or int(task['fencing_token']) != 1 or int(task['row_version']) != 3 or task['lease_until'] is not None or row['deleted'] is not (not failed) or row['remaining_rows'] != (1 if failed else 0) or task['result_reference'] != (None if failed else op['ID'] + '-deleted'):
            raise ValueError('registered consumer actual effect/lease progression differs')
    negative = sample['negative_operation']
    operation_digest(negative)
    if negative != operations[sample['revoked']['DocumentID']] or sample['before'] != {'OperationID': negative['ID'], 'RequestDigest': negative['Digest'], 'State': 'absent', 'ResultReference': ''}:
        raise ValueError('revoked executor initial actual state differs')
    if sample['failed']['Job']['ID'] != sample['revoked']['JobID'] or sample['failed']['Job']['State'] != 'failed' or sample['failed']['Job']['Error']['Retryable'] is not False or sample['cycle'] != {'Discovered': 1, 'Attempted': 1, 'Completed': 1, 'Failed': 0, 'WorkErrors': 0, 'SweepCompleted': True}:
        raise ValueError('registered queue outcomes differ')
    for guard in ('removed_binding_denied', 'wrong_implementation_denied', 'unknown_runtime_denied', 'wrong_profile_denied', 'closed_runtime_denied', 'sealed_mutation_denied', 'head_regression_denied', 'unsealed_activation_denied'):
        if sample[guard] is not True:
            raise ValueError('registry guard missing: ' + guard)
    if sample['worker_authority'] != 'actual-rs256-service-and-sealed-pg-grants' or sample['source_authority'] != 'fixture-metadata-and-governance' or sample['deployment_provisioning'] != 'host-plan-and-test-operator-catalog':
        raise ValueError('registry provenance differs')
    if source_inventory() != sources:
        raise ValueError('registry sources changed during independent review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'scope': 'PG16 current sealed deployment catalogue and named kernel assembly, independent config/operation digests, retained profile/provider/implementation/runtime pins, per-call revocation and closed escaped executor denial, actual native index effect under RS256 service/current ACL; host/test operator provisioning and metadata fixture, no complete deployment attestation/bootstrap/remote Grant/write barrier/full V1 acceptance'}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/cleanup-registry-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 cleanup registry: sealed deployment, named runtime, pins, revocation and actual effect independently verified')

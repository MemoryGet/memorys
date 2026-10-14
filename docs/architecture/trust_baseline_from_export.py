"""Bind a typed TrustHead start inventory to explicit verified export bytes."""
import hashlib
import json
from canonical_content import canonical
from check_config import LIMIT,object_pairs,finite_float,reject_constant
from export_validation import validate_package
from journal_validation import validate_bundle

EXPECTED_KEYS={'export_id','deployment_id','build_lock_sha256','metadata_contract_version','snapshot_id',
               'journal_stream_id','committed_watermark','manifest_sha256'}


def build_baseline(raw_manifest,payloads,expected):
    """expected is independent control-plane/snapshot identity, not manifest-derived."""
    if not isinstance(raw_manifest,bytes) or len(raw_manifest)>LIMIT:
        raise ValueError('trust_export_manifest_bytes_required')
    if not isinstance(expected,dict) or set(expected)!=EXPECTED_KEYS:
        raise ValueError('trust_export_expected_identity_required')
    canonical(raw_manifest)
    manifest=json.loads(raw_manifest.decode('utf-8'),object_pairs_hook=object_pairs,parse_float=finite_float,parse_constant=reject_constant)
    if not isinstance(payloads,dict):raise ValueError('trust_export_payloads_required')
    # bytes values are immutable; capture one mapping and consume exactly those validated bytes.
    frozen_payloads=dict(payloads)
    report=validate_package(manifest,frozen_payloads)
    identity={'export_id':manifest['export_id'],'deployment_id':manifest['source']['deployment_id'],
              'build_lock_sha256':manifest['source']['build_lock_sha256'],'metadata_contract_version':manifest['source']['metadata_contract_version'],
              'snapshot_id':manifest['snapshot']['snapshot_id'],'journal_stream_id':manifest['snapshot']['journal_stream_id'],
              'committed_watermark':manifest['snapshot']['committed_watermark'],'manifest_sha256':hashlib.sha256(raw_manifest).hexdigest()}
    if identity!=expected:raise ValueError('trust_export_identity_mismatch')
    heads={}
    for segment in manifest['segments']:
        if segment['resource_type']!='trust_head':continue
        for line in frozen_payloads[segment['entry_id']].splitlines():
            row=json.loads(line.decode('utf-8'),object_pairs_hook=object_pairs,parse_float=finite_float,parse_constant=reject_constant)
            heads[row['record_id']]=row['payload']
    if len(heads)>10000:raise ValueError('trust_export_baseline_limit')
    baseline={'snapshot_id':identity['snapshot_id'],'journal_stream_id':identity['journal_stream_id'],
              'from_cursor':identity['committed_watermark'],'heads':heads}
    return baseline,{'export_identity':identity,'typed_head_count':len(heads),'ready_for_replay':False,
                     'export_report':report,'unverified':['expected_identity_provenance','actual_snapshot_and_inventory',
                     'external_control_anchor','other_typed_records','atomic_database_replay']}


def validate_journal_from_export(raw_manifest,export_payloads,expected_export,bundle,journal_payloads,expected_journal):
    baseline,source=build_baseline(raw_manifest,export_payloads,expected_export)
    report=validate_bundle(bundle,journal_payloads,expected_journal,baseline)
    report['trust_source_export']=source
    return report

"""Offline hash-chain checks; does not allocate watermarks, replay, or authorize cutover."""
import hashlib
import json
import copy

from jsonschema import Draft202012Validator

from build_journal_contract import build
from canonical_content import canonical
from check_config import LIMIT, check_depth, finite_float, object_pairs, reject_constant
from export_validation import validate_unicode
from build_trust_head import validate_record as validate_trust_head,validate_advance as validate_trust_advance


def transaction_cursor(transaction):
    core={key:value for key,value in transaction.items() if key!="cursor"}
    data=json.dumps(core,ensure_ascii=False,allow_nan=False).encode("utf-8")
    return "j1:"+hashlib.sha256(b"akos.journal/1\x00"+canonical(data)).hexdigest()


def validate_bundle(bundle,payloads,expected,trust_snapshot=None):
    """expected must come from independently held snapshot/head, not this bundle."""
    if not isinstance(payloads,dict) or len(payloads)>10000:
        raise ValueError("journal_offline_payload_limit")
    if isinstance(bundle,dict) and isinstance(bundle.get("transactions"),list) and len(bundle["transactions"])>1000:
        raise ValueError("journal_offline_transaction_limit")
    schema=build();Draft202012Validator.check_schema(schema);Draft202012Validator(schema).validate(bundle)
    if type(bundle["schema_version"]) is not int:
        raise ValueError("journal_integer_required")
    if not isinstance(expected,dict) or set(expected)!={"journal_stream_id","from_cursor","through_cursor"}:
        raise ValueError("journal_expected_boundary_required")
    if any(bundle[key]!=value for key,value in expected.items()):
        raise ValueError("journal_expected_boundary_mismatch")
    previous=bundle["from_cursor"];cursors={previous};transactions=set();entries=set();versions={};total_bytes=0
    trust_heads={};typed_counts={"trust_head":0};initial_trust_count=0
    if trust_snapshot is not None:
        if not isinstance(trust_snapshot,dict) or set(trust_snapshot)!={"snapshot_id","journal_stream_id","from_cursor","heads"}:
            raise ValueError("journal_trust_snapshot_required")
        if not isinstance(trust_snapshot["snapshot_id"],str) or not trust_snapshot["snapshot_id"] or len(trust_snapshot["snapshot_id"])>200:
            raise ValueError("journal_trust_snapshot_required")
        if any(trust_snapshot[k]!=expected[k] for k in ("journal_stream_id","from_cursor")):
            raise ValueError("journal_trust_snapshot_boundary_mismatch")
        heads=trust_snapshot["heads"]
        if not isinstance(heads,dict) or len(heads)>10000:raise ValueError("journal_trust_snapshot_required")
        for record_id,head in heads.items():
            validate_trust_head(head,record_id)
            trust_heads[("trust_head",record_id)]=copy.deepcopy(head)
        initial_trust_count=len(heads)
    for transaction in bundle["transactions"]:
        mutations=transaction["mutations"]
        if len(mutations)>10000:
            raise ValueError("journal_offline_mutation_limit")
        if transaction["journal_stream_id"]!=bundle["journal_stream_id"] or transaction["previous_cursor"]!=previous:
            raise ValueError("journal_chain_boundary_mismatch")
        if transaction["transaction_id"] in transactions or transaction["cursor"] in cursors:
            raise ValueError("journal_duplicate_transaction")
        if type(transaction["mutation_count"]) is not int or transaction["mutation_count"]!=len(mutations):
            raise ValueError("journal_incomplete_transaction")
        if transaction["cursor"]!=transaction_cursor(transaction):
            raise ValueError("journal_cursor_digest_mismatch")
        resources=set()
        for ordinal,change in enumerate(mutations):
            for value in (change["ordinal"],change["new_version"],change["payload"]["byte_length"]):
                if type(value) is not int:raise ValueError("journal_integer_required")
            old=change["previous_version"]
            if old is not None and (type(old) is not int or change["new_version"]<=old):
                raise ValueError("journal_version_not_increasing")
            if change["ordinal"]!=ordinal:raise ValueError("journal_ordinal_mismatch")
            key=(change["record_type"],change["record_id"])
            if key in resources:raise ValueError("journal_duplicate_resource")
            resources.add(key)
            if key in versions and old!=versions[key]:raise ValueError("journal_version_chain_mismatch")
            versions[key]=change["new_version"]
            reference=change["payload"];entry=reference["entry_id"]
            if entry in entries:raise ValueError("journal_duplicate_payload_reference")
            entries.add(entry)
            if entry not in payloads or not isinstance(payloads[entry],bytes):raise ValueError("journal_missing_payload")
            data=payloads[entry];total_bytes+=len(data)
            if len(data)>LIMIT or total_bytes>32*LIMIT:raise ValueError("journal_offline_byte_limit")
            if len(data)!=reference["byte_length"] or hashlib.sha256(data).hexdigest()!=reference["sha256"]:
                raise ValueError("journal_payload_digest_mismatch")
            text=data.decode("utf-8");check_depth(text)
            row=json.loads(text,object_pairs_hook=object_pairs,parse_constant=reject_constant,parse_float=finite_float)
            validate_unicode(row)
            if not isinstance(row,dict) or set(row)!={"record_type","record_id","effect","payload"} or not isinstance(row["payload"],dict):
                raise ValueError("journal_payload_envelope_invalid")
            if any(row[k]!=change[k] for k in ("record_type","record_id","effect")):
                raise ValueError("journal_payload_target_mismatch")
            if change["record_type"]=="trust_head":
                if trust_snapshot is None:raise ValueError("journal_trust_snapshot_required")
                if change["effect"]!="upsert":raise ValueError("journal_trust_head_tombstone_forbidden")
                head=row["payload"];validate_trust_head(head,change["record_id"])
                if head["row_version"]!=change["new_version"] or (old is None and head["row_version"]!=1) or (old is not None and head["row_version"]!=old+1):
                    raise ValueError("journal_trust_head_version_mismatch")
                if key in trust_heads:
                    if old!=trust_heads[key]["row_version"]:raise ValueError("journal_trust_snapshot_version_mismatch")
                    validate_trust_advance(trust_heads[key],head)
                elif old is not None:
                    raise ValueError("journal_trust_snapshot_missing_record")
                trust_heads[key]=head;typed_counts["trust_head"]+=1
        transactions.add(transaction["transaction_id"]);cursors.add(transaction["cursor"]);previous=transaction["cursor"]
    if previous!=bundle["through_cursor"]:raise ValueError("journal_incomplete_range")
    if set(payloads)!=entries:raise ValueError("journal_payload_coverage_mismatch")
    return {"status":"offline_journal_chain_validated","ready_for_replay":False,"transaction_count":len(transactions),
            "mutation_count":len(entries),"typed_mutation_counts":typed_counts,"initial_trust_head_count":initial_trust_count,"unverified":["trusted_committed_head","actual_atomic_commit_order","trust_snapshot_provenance_and_inventory",
            "typed_mutation_payloads","snapshot_initial_versions","atomic_replay_receipts","current_authorization",
            "external_blobs_and_indexes","cutover_transaction"]}

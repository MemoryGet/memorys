"""Offline, bounded manifest/envelope checks; never import or mark data ready."""
import hashlib
import json

from jsonschema import Draft202012Validator

from build_export_manifest import RESOURCE_TYPES, build
from check_config import LIMIT, check_depth, finite_float, object_pairs, reject_constant


def validate_package(manifest, payloads):
    """payloads is an explicit entry-id -> bytes mapping, not paths or URLs."""
    if not isinstance(payloads, dict) or len(payloads) > 1000:
        raise ValueError("export_offline_segment_limit")
    if isinstance(manifest, dict) and isinstance(manifest.get("segments"), list) and len(manifest["segments"]) > 1000:
        raise ValueError("export_offline_segment_limit")
    schema = build()
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(manifest)
    if type(manifest["schema_version"]) is not int:
        raise ValueError("export_integer_required")
    if len(payloads) > 1000 or len(manifest["segments"]) > 1000:
        raise ValueError("export_offline_segment_limit")
    entries, resources, record_keys = set(), set(), set()
    total_bytes = 0
    for segment in manifest["segments"]:
        entry = segment["entry_id"]
        if entry in entries:
            raise ValueError("export_duplicate_entry")
        entries.add(entry)
        resource = segment["resource_type"]
        resources.add(resource)
        if segment["record_contract"] != f"akos.metadata.{resource}/1":
            raise ValueError("export_record_contract_mismatch")
        for key in ("byte_length", "record_count"):
            if type(segment[key]) is not int:
                raise ValueError("export_integer_required")
        if entry not in payloads or not isinstance(payloads[entry], bytes):
            raise ValueError("export_missing_payload")
        data = payloads[entry]
        total_bytes += len(data)
        if len(data) > LIMIT or total_bytes > 32 * LIMIT:
            raise ValueError("export_offline_byte_limit")
        if len(data) != segment["byte_length"] or hashlib.sha256(data).hexdigest() != segment["sha256"]:
            raise ValueError("export_payload_digest_mismatch")
        if data and not data.endswith(b"\n"):
            raise ValueError("export_record_terminator_missing")
        lines = data.split(b"\n")[:-1] if data else []
        if len(lines) != segment["record_count"]:
            raise ValueError("export_record_count_mismatch")
        for line in lines:
            if not line or b"\r" in line:
                raise ValueError("export_record_line_invalid")
            text = line.decode("utf-8")
            check_depth(text)
            row = json.loads(text, object_pairs_hook=object_pairs, parse_constant=reject_constant, parse_float=finite_float)
            validate_unicode(row)
            if not isinstance(row, dict) or set(row) != {"record_type", "record_id", "payload"}:
                raise ValueError("export_record_envelope_invalid")
            if row["record_type"] != resource or not isinstance(row["record_id"], str) or not row["record_id"] or len(row["record_id"]) > 200 or not isinstance(row["payload"], dict):
                raise ValueError("export_record_envelope_invalid")
            key = (resource, row["record_id"])
            if key in record_keys:
                raise ValueError("export_duplicate_record")
            record_keys.add(key)
    if resources != set(RESOURCE_TYPES) or set(payloads) != entries:
        raise ValueError("export_coverage_mismatch")
    return {"status": "offline_manifest_envelopes_validated", "ready_for_import": False,
            "segment_count": len(entries), "record_count": len(record_keys),
            "unverified": ["trusted_package_provenance", "complete_authoritative_snapshot", "journal_commit_watermark",
                           "typed_record_payloads", "foreign_keys_and_space_ownership", "frozen_content_hashes",
                           "external_blob_bytes", "lease_invalidation", "backend_epoch_compatibility", "import_transactions"]}


def validate_unicode(value):
    """Escaped lone surrogates are accepted by json.loads but invalid platform text."""
    if isinstance(value, str):
        value.encode("utf-8", errors="strict")
    elif isinstance(value, list):
        for item in value:
            validate_unicode(item)
    elif isinstance(value, dict):
        for key, item in value.items():
            validate_unicode(key)
            validate_unicode(item)

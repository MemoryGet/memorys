"""Generate the metadata snapshot manifest design; payload schemas remain separate."""
import argparse
import json
from pathlib import Path


ROOT = Path(__file__).parent
METADATA_CONTRACT_VERSION = "1.1"
ADDED_RESOURCE_TYPES = (
    "upload_operation", "trust_domain", "trust_head", "trust_snapshot", "key_retirement",
    "trust_activation", "runtime_trust_receipt", "deployment_audit_record", "deployment_outbox_record"
)
RESOURCE_TYPES = (
    "space", "knowledge_base", "principal", "identity_binding", "membership", "kb_grant", "authorization_policy",
    "document", "draft", "draft_block", "draft_blob", "revision", "block", "blob", "storage_binding",
    "revision_blob", "approval_request", "approval", "publication_operation", "publication_history", "backend_epoch",
    "generation_set", "index_generation", "backend_binding", "profile_snapshot", "job", "job_attempt",
    "outbox", "consumer_receipt", "idempotency_record", "audit_record", "migration_journal",
    "feedback", "draft_proposal", "memory_record", "retention_hold", "purge_job", "migration_operation",
    "tombstone", "request_record", "invocation_record", "immutable_payload", "credential_reference", "retention_policy_snapshot"
) + ADDED_RESOURCE_TYPES


def build():
    identifier = {"type": "string", "minLength": 1, "maxLength": 200, "pattern": "^[a-zA-Z0-9][a-zA-Z0-9._:-]*$"}
    digest = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
    counter = {"type": "integer", "minimum": 0, "maximum": 9223372036854775807}
    def obj(properties):
        return {"type": "object", "additionalProperties": False, "required": list(properties), "properties": properties}
    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "title": "AI Knowledge OS full metadata snapshot ExportManifest V1",
        "description": "Manifest and record-envelope design only. Foreign keys, payload contracts, actual committed snapshot and import transactions require additional verification.",
        **obj({
            "schema_version": {"const": 1}, "export_id": identifier,
            "kind": {"const": "metadata_full_snapshot"},
            "source": obj({"deployment_id": identifier, "build_lock_sha256": digest, "metadata_contract_version": {"const": METADATA_CONTRACT_VERSION}}),
            "snapshot": obj({"snapshot_id": identifier, "journal_stream_id": identifier,
                             "committed_watermark": identifier, "boundary": {"const": "after_complete_transaction"},
                             "writes": {"const": "all_metadata_writes_paused"}}),
            "segments": {"type": "array", "minItems": len(RESOURCE_TYPES), "items": obj({
                "entry_id": identifier, "resource_type": {"enum": list(RESOURCE_TYPES)},
                "record_contract": {"type": "string", "pattern": "^akos\\.metadata\\.[a-z_]+/1$"},
                "encoding": {"const": "jsonl-envelope/1"}, "record_count": counter,
                "byte_length": counter, "sha256": digest})},
            "blob_payloads": {"const": "external_required_before_cutover"},
            "derived_indexes": {"const": "excluded_rebuild_or_verify"},
            "lease_restore": {"const": "invalidate_all_inflight"}
        })
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    target = ROOT / "schemas/export-manifest.v1.schema.json"
    encoded = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.write:
        target.write_text(encoded, encoding="utf-8")
    else:
        if target.read_text(encoding="utf-8") != encoded:
            raise SystemExit("export manifest schema is stale")
        print("PASS: export manifest schema generation consistency")


if __name__ == "__main__":
    main()

"""Upload operation record schema; no runtime confirmation authority."""
import argparse
import json
from pathlib import Path


def build():
    ident = {"type": "string", "pattern": "^[a-zA-Z0-9][a-zA-Z0-9._:-]*$", "maxLength": 200}
    digest = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
    version = {"type": "integer", "minimum": 1, "maximum": 18446744073709551615}
    size = {"type": "integer", "minimum": 0, "maximum": 9223372036854775807}
    def obj(props):
        return {"type": "object", "additionalProperties": False, "required": list(props), "properties": props}
    def nullable(value):
        return {"anyOf": [value, {"type": "null"}]}
    content = obj({"sha256": digest, "size": size, "media_type": {"type": "string", "minLength": 1, "maxLength": 255}})
    target = {"oneOf": [obj({"kind": {"const": "create_draft"}, "kb_id": ident}),
                        obj({"kind": {"const": "replace_original"}, "kb_id": ident, "draft_id": ident, "expected_row_version": version})]}
    candidate = obj({"attempt_id": ident, "blob_id": ident, "storage_binding_ref": ident,
                     "storage_epoch_id": ident, "object_version_ref": ident, "content": content})
    attempt = obj({"attempt_id": ident, "fencing_token": version, "lease_owner_id": ident,
                   "lease_until_seconds": size})
    result = obj({"blob_id": ident, "draft_id": ident, "draft_row_version": version,
                  "job_id": ident, "transaction_id": ident, "journal_cursor": {"type": "string", "pattern": "^j1:[a-f0-9]{64}$"}})
    error = obj({"code": {"enum": ["ACCESS_DENIED", "CONFLICT", "CANCELLED", "INVALID_INPUT", "INTEGRITY_FAILED", "DEADLINE_EXCEEDED", "INTERNAL"]}})
    return {"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "AI Knowledge OS UploadOperation V1",
            "description": "Internal record only: claims are not proof of current authorization, durable bytes or committed transaction.",
            **obj({"schema_version": {"const": 1}, "kind": {"const": "upload_operation"},
                   "operation_id": ident, "space_id": ident, "subject_id": ident,
                   "idempotency_key": {"type": "string", "minLength": 1, "maxLength": 200},
                   "semantic_digest": digest, "profile_digest": digest, "target": target, "content": content,
                   "state": {"enum": ["accepted", "receiving", "validated", "materializing", "prepared", "reconciling", "committed", "rejected"]},
                   "row_version": version, "fencing_high_water": {"type": "integer", "minimum": 0, "maximum": 18446744073709551615},
                   "retention_hold_id": ident, "audit_reference": ident,
                   "cancel_requested": {"type": "boolean"}, "attempt": nullable(attempt), "candidate": nullable(candidate),
                   "unknown_result": {"enum": [None, "object_write", "metadata_commit"]},
                   "result": nullable(result), "error": nullable(error)})}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    path = Path(__file__).parent / "schemas/upload-operation.v1.schema.json"
    text = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.write:
        path.write_text(text, encoding="utf-8")
    elif path.read_text(encoding="utf-8") != text:
        raise SystemExit("upload operation schema is stale")
    else:
        print("PASS: upload operation schema generation consistency")

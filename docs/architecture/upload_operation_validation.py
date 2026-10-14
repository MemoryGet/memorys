"""Read-only record checks, never persist/authorize/confirm/delete an upload."""
from jsonschema import Draft202012Validator
from build_upload_operation import build

IDENTITY = ("operation_id", "space_id", "subject_id", "idempotency_key", "semantic_digest",
            "profile_digest", "target", "content")


def validate_record(record, expected):
    schema = build()
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(record)
    def exact(value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key in {"schema_version", "row_version", "expected_row_version", "size", "fencing_token", "fencing_high_water", "lease_until_seconds", "draft_row_version"}:
                    if type(item) is not int:
                        raise ValueError("upload_integer_required")
                exact(item)
    exact(record)
    if not isinstance(expected, dict) or set(expected) != set(IDENTITY):
        raise ValueError("upload_independent_expectation_required")
    exact(expected)
    if any(record[k] != expected[k] for k in IDENTITY):
        raise ValueError("upload_identity_mismatch")
    state = record["state"]
    attempt, candidate, result, error = (record[k] for k in ("attempt", "candidate", "result", "error"))
    unknown = record["unknown_result"]
    if state == "accepted" and (attempt is not None or candidate is not None):
        raise ValueError("upload_premature_artifact")
    if state not in {"accepted", "rejected"} and attempt is None:
        raise ValueError("upload_attempt_required")
    if attempt is not None and attempt["fencing_token"] != record["fencing_high_water"]:
        raise ValueError("upload_fencing_mismatch")
    if state in {"receiving", "validated", "materializing"} and candidate is not None:
        raise ValueError("upload_premature_artifact")
    if state in {"prepared", "committed"} and candidate is None:
        raise ValueError("upload_candidate_required")
    if candidate is not None:
        if attempt is None or candidate["attempt_id"] != attempt["attempt_id"] or candidate["content"] != record["content"]:
            raise ValueError("upload_candidate_mismatch")
    if state == "reconciling":
        if unknown is None or (unknown == "metadata_commit" and candidate is None):
            raise ValueError("upload_unknown_context_required")
    elif unknown is not None:
        raise ValueError("upload_premature_unknown")
    if state == "committed":
        if result is None or error is not None or result["blob_id"] != candidate["blob_id"]:
            raise ValueError("upload_commit_result_mismatch")
        target = record["target"]
        if target["kind"] == "replace_original" and (result["draft_id"] != target["draft_id"] or result["draft_row_version"] != target["expected_row_version"] + 1):
            raise ValueError("upload_draft_result_mismatch")
    elif result is not None:
        raise ValueError("upload_premature_result")
    if (state == "rejected") != (error is not None):
        raise ValueError("upload_error_state_mismatch")
    return {"operation_id": record["operation_id"], "state": state, "ready_for_confirmation": False,
            "unverified": ["semantic_digest_recomputation", "current_authorization", "lease_and_hold", "immutable_object_bytes",
                           "storage_epoch", "transaction_commit", "recovery_and_cleanup"]}

"""Offline retention design checks; no storage access or deletion authorization."""
import json
from pathlib import Path

from jsonschema import Draft202012Validator


SCHEMA_PATH = Path(__file__).parent / "schemas" / "retention-policy.v1.schema.json"


def validate_policy(policy):
    """Check policy structure and duration relationships. Return no purge permit."""
    schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(policy)
    # JSON Schema accepts mathematically integral floats; persistent seconds use
    # exact signed 64-bit integers and must not accept rounded float input.
    for name, value in policy.items():
        if name.endswith("_seconds") or name in ("schema_version", "policy_version"):
            if type(value) is not int:
                raise ValueError("retention_integer_required")
    horizon = (policy["rollback_window_seconds"] + policy["replay_budget_seconds"]
               + policy["recovery_margin_seconds"])
    if horizon > 9223372036854775807:
        raise ValueError("retention_horizon_overflow")
    for name in ("journal_min_seconds", "original_min_seconds"):
        if policy[name] < horizon:
            raise ValueError("retention_rollback_horizon_short")
    if policy["idempotency_min_seconds"] < policy["retry_window_seconds"]:
        raise ValueError("retention_retry_window_short")
    attempt_horizon = policy["max_attempt_seconds"] + policy["cancel_settle_seconds"]
    if attempt_horizon > 9223372036854775807:
        raise ValueError("retention_attempt_horizon_overflow")
    if policy["orphan_grace_seconds"] < attempt_horizon:
        raise ValueError("retention_orphan_grace_short")

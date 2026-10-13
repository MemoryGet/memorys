"""Pure policy rejection tests; do not exercise a GC worker or database."""
import copy
import json
from pathlib import Path
import unittest

from jsonschema import ValidationError

from retention_validation import validate_policy


class RetentionContractTests(unittest.TestCase):
    def setUp(self):
        self.policy = json.loads((Path(__file__).parent / "examples" /
                                  "design-retention-policy.json").read_text(encoding="utf-8"))

    def reject(self, key, value, error):
        candidate = copy.deepcopy(self.policy)
        candidate[key] = value
        with self.assertRaises(error):
            validate_policy(candidate)

    def test_exact_boundary_is_valid_but_returns_no_purge_permit(self):
        self.assertIsNone(validate_policy(self.policy))

    def test_journal_and_original_must_include_replay_and_recovery(self):
        horizon = sum(self.policy[k] for k in (
            "rollback_window_seconds", "replay_budget_seconds", "recovery_margin_seconds"))
        for key in ("journal_min_seconds", "original_min_seconds"):
            self.reject(key, horizon - 1, ValueError)
            self.reject(key, self.policy["rollback_window_seconds"], ValueError)

    def test_retry_result_cannot_expire_within_supported_retry_window(self):
        self.reject("idempotency_min_seconds", self.policy["retry_window_seconds"] - 1, ValueError)

    def test_orphan_grace_includes_cancel_settlement(self):
        self.reject("orphan_grace_seconds", self.policy["max_attempt_seconds"], ValueError)

    def test_no_implicit_zero_unbounded_negative_or_unknown_policy(self):
        for value in (0, -1, None, "forever", True, 1.5):
            self.reject("audit_min_seconds", value, ValidationError)
        self.reject("auto_purge", True, ValidationError)
        missing = copy.deepcopy(self.policy)
        missing.pop("audit_min_seconds")
        with self.assertRaises(ValidationError):
            validate_policy(missing)

    def test_integral_floats_and_noninteger_versions_rejected(self):
        for key in ("schema_version", "policy_version", "audit_min_seconds"):
            self.reject(key, float(self.policy[key]), ValueError)

    def test_signed_integer_horizon_overflow_rejected(self):
        for key in ("rollback_window_seconds", "max_attempt_seconds"):
            self.reject(key, 9223372036854775807, ValueError)
        self.reject("audit_min_seconds", 9223372036854775808, ValidationError)


if __name__ == "__main__":
    unittest.main()

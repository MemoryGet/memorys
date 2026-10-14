"""Synthetic record evidence only; no storage or database transaction tests."""
import copy
import unittest
from jsonschema import ValidationError
from upload_operation_validation import IDENTITY, validate_record


class UploadOperationTests(unittest.TestCase):
    def setUp(self):
        self.record = {"schema_version": 1, "kind": "upload_operation", "operation_id": "upload-1",
                       "space_id": "space-1", "subject_id": "user-1", "idempotency_key": "key-1",
                       "semantic_digest": "a" * 64, "profile_digest": "b" * 64,
                       "target": {"kind": "replace_original", "kb_id": "kb-1", "draft_id": "draft-1", "expected_row_version": 3},
                       "content": {"sha256": "c" * 64, "size": 9007199254740993, "media_type": "application/octet-stream"},
                       "state": "accepted", "row_version": 1, "fencing_high_water": 0, "retention_hold_id": "hold-1", "audit_reference": "audit-1",
                       "cancel_requested": False, "attempt": None, "candidate": None, "unknown_result": None, "result": None, "error": None}
        self.expected = {k: copy.deepcopy(self.record[k]) for k in IDENTITY}

    def prepare(self):
        self.record.update(state="prepared", fencing_high_water=4, attempt={"attempt_id": "attempt-1", "fencing_token": 4,
                           "lease_owner_id": "worker-1", "lease_until_seconds": 1000})
        self.record["candidate"] = {"attempt_id": "attempt-1", "blob_id": "blob-1", "storage_binding_ref": "binding-1",
                                    "storage_epoch_id": "epoch-1", "object_version_ref": "object-version-1",
                                    "content": copy.deepcopy(self.record["content"])}

    def commit(self):
        self.prepare()
        self.record.update(state="committed", result={"blob_id": "blob-1", "draft_id": "draft-1", "draft_row_version": 4,
                           "job_id": "job-1", "transaction_id": "tx-1", "journal_cursor": "j1:" + "d" * 64})

    def test_all_eight_states_remain_non_authoritative(self):
        for state in ("accepted", "receiving", "validated", "materializing", "prepared", "reconciling", "committed", "rejected"):
            with self.subTest(state=state):
                self.setUp()
                if state == "committed": self.commit()
                elif state in {"prepared", "reconciling"}: self.prepare()
                elif state in {"receiving", "validated", "materializing"}:
                    self.prepare(); self.record["candidate"] = None
                self.record["state"] = state
                if state == "reconciling": self.record["unknown_result"] = "metadata_commit"
                if state == "rejected": self.record["error"] = {"code": "CANCELLED"}
                self.assertFalse(validate_record(self.record, self.expected)["ready_for_confirmation"])

    def test_independent_identity_and_target_mismatch(self):
        for field in ("space_id", "subject_id", "semantic_digest", "profile_digest", "idempotency_key"):
            candidate = copy.deepcopy(self.record)
            candidate[field] = "e" * 64 if field.endswith("digest") else "foreign"
            with self.assertRaisesRegex(ValueError, "identity_mismatch"): validate_record(candidate, self.expected)
        self.record["target"]["expected_row_version"] = 4
        with self.assertRaisesRegex(ValueError, "identity_mismatch"): validate_record(self.record, self.expected)

    def test_float_and_boolean_integers_and_overflow_rejected(self):
        for value in (1.0, True, 9223372036854775808):
            candidate = copy.deepcopy(self.record); candidate["content"]["size"] = value
            expected = copy.deepcopy(self.expected); expected["content"]["size"] = value
            with self.assertRaises((ValueError, ValidationError)): validate_record(candidate, expected)
        self.assertFalse(validate_record(self.record, self.expected)["ready_for_confirmation"])

    def test_untrusted_location_and_error_text_rejected(self):
        self.prepare(); self.record["candidate"]["download_url"] = "https://invalid.example/private"
        with self.assertRaises(ValidationError): validate_record(self.record, self.expected)
        self.setUp(); self.record.update(state="rejected", error={"code": "INTERNAL", "message": "SECRET"})
        with self.assertRaises(ValidationError): validate_record(self.record, self.expected)

    def test_premature_and_mismatched_candidate_or_result(self):
        self.prepare(); self.record["state"] = "receiving"
        with self.assertRaisesRegex(ValueError, "premature_artifact"): validate_record(self.record, self.expected)
        self.prepare(); self.record["candidate"]["attempt_id"] = "old-attempt"
        with self.assertRaisesRegex(ValueError, "candidate_mismatch"): validate_record(self.record, self.expected)
        self.prepare(); self.record["candidate"]["content"]["sha256"] = "f" * 64
        with self.assertRaisesRegex(ValueError, "candidate_mismatch"): validate_record(self.record, self.expected)
        self.commit(); self.record["result"]["blob_id"] = "foreign"
        with self.assertRaisesRegex(ValueError, "commit_result_mismatch"): validate_record(self.record, self.expected)
        self.commit(); self.record["result"]["draft_row_version"] = 3
        with self.assertRaisesRegex(ValueError, "draft_result_mismatch"): validate_record(self.record, self.expected)

    def test_unknown_commit_requires_candidate_and_never_success(self):
        self.prepare(); self.record.update(state="reconciling", unknown_result="metadata_commit", candidate=None)
        with self.assertRaisesRegex(ValueError, "unknown_context_required"): validate_record(self.record, self.expected)
        self.prepare(); self.record.update(state="reconciling", unknown_result="object_write", candidate=None)
        self.assertFalse(validate_record(self.record, self.expected)["ready_for_confirmation"])
        self.commit(); self.record["state"] = "reconciling"; self.record["unknown_result"] = "metadata_commit"
        with self.assertRaisesRegex(ValueError, "premature_result"): validate_record(self.record, self.expected)

    def test_terminal_error_and_expectation_shape(self):
        self.record.update(state="rejected", error=None)
        with self.assertRaisesRegex(ValueError, "error_state_mismatch"): validate_record(self.record, self.expected)
        self.setUp(); self.expected["authority"] = True
        with self.assertRaisesRegex(ValueError, "independent_expectation_required"): validate_record(self.record, self.expected)


if __name__ == "__main__": unittest.main()

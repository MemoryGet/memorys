"""Synthetic facts exercise transition rules, not real CAS or crash recovery."""
import copy
import unittest
import test_upload_operation as fixtures
from upload_transition_validation import FLAGS, validate_transition


class UploadTransitionTests(unittest.TestCase):
    def setUp(self):
        fixture = fixtures.UploadOperationTests(); fixture.setUp(); self.accepted = copy.deepcopy(fixture.record); self.expected = fixture.expected
        fixture.commit(); self.committed = copy.deepcopy(fixture.record)
        self.prepared = copy.deepcopy(self.committed); self.prepared.update(state="prepared", result=None)
        self.facts = {k: True for k in FLAGS}; self.facts.update(now_seconds=100, expected_row_version=1)
        self.facts["commit_absent_observed"] = False

    def check(self, before, after):
        after["row_version"] = before["row_version"] + 1
        self.facts["expected_row_version"] = before["row_version"]
        return validate_transition(before, after, self.expected, self.facts)

    def receiving(self):
        r = copy.deepcopy(self.prepared); r.update(state="receiving", candidate=None)
        return r

    def unknown(self, kind="metadata_commit"):
        r = copy.deepcopy(self.prepared); r.update(state="reconciling", unknown_result=kind)
        if kind == "object_write": r["candidate"] = None
        return r

    def test_forward_path_and_unknown_commit_recovery(self):
        records = [copy.deepcopy(self.accepted), self.receiving()]
        for state in ("validated", "materializing"):
            r = self.receiving(); r["state"] = state; records.append(r)
        records += [copy.deepcopy(self.prepared), self.unknown(), copy.deepcopy(self.committed)]
        for before, after in zip(records, records[1:]):
            self.assertFalse(self.check(before, after)["ready_for_confirmation"])

    def test_row_version_identity_and_terminal_changes_rejected(self):
        r = self.receiving(); r["row_version"] = 3
        with self.assertRaisesRegex(ValueError, "row_version_mismatch"):
            validate_transition(self.accepted, r, self.expected, self.facts)
        r = self.receiving(); r["retention_hold_id"] = "foreign"
        with self.assertRaisesRegex(ValueError, "identity_changed"): self.check(self.accepted, r)
        with self.assertRaisesRegex(ValueError, "invalid_edge"): self.check(self.committed, copy.deepcopy(self.prepared))

    def test_fencing_survives_reset_and_new_attempt_must_advance(self):
        reset = copy.deepcopy(self.accepted); reset["fencing_high_water"] = 4
        self.assertFalse(self.check(self.unknown("object_write"), reset)["ready_for_confirmation"])
        with self.assertRaisesRegex(ValueError, "fencing_not_new"): self.check(reset, self.receiving())
        r = self.receiving(); r["fencing_high_water"] = 5; r["attempt"]["fencing_token"] = 5; r["attempt"]["attempt_id"] = "attempt-2"
        self.assertFalse(self.check(reset, r)["ready_for_confirmation"])
        reset["fencing_high_water"] = 0
        with self.assertRaisesRegex(ValueError, "fencing_regressed"): self.check(self.unknown("object_write"), reset)

    def test_cancel_intent_is_monotone_and_blocks_new_commit(self):
        canceled = copy.deepcopy(self.prepared); canceled["cancel_requested"] = True
        self.assertFalse(self.check(self.prepared, canceled)["ready_for_confirmation"])
        commit = copy.deepcopy(self.committed); commit["cancel_requested"] = True
        with self.assertRaisesRegex(ValueError, "cancel_pending"): self.check(canceled, commit)
        cleared = copy.deepcopy(self.prepared)
        with self.assertRaisesRegex(ValueError, "cancel_cleared"): self.check(canceled, cleared)
        # A trusted observation of an earlier commit records facts, never re-executes it.
        unknown = self.unknown(); unknown["cancel_requested"] = True
        self.assertFalse(self.check(unknown, commit)["ready_for_confirmation"])

    def test_missing_guards_expired_lease_and_forged_facts_rejected(self):
        for name in ("hold_active", "actor_authorized", "writers_enabled", "draft_matches", "object_verified", "storage_epoch_matches", "commit_observed"):
            self.facts[name] = False
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "guard_failed"):
                self.check(self.prepared, copy.deepcopy(self.committed))
            self.facts[name] = True
        self.facts["now_seconds"] = 1000
        with self.assertRaisesRegex(ValueError, "lease_expired"): self.check(self.prepared, copy.deepcopy(self.committed))
        self.facts["now_seconds"] = 100; self.facts["hold_active"] = 1
        with self.assertRaisesRegex(ValueError, "boolean_required"): self.check(self.prepared, copy.deepcopy(self.committed))

    def test_unknown_results_cannot_be_assumed_absent(self):
        self.facts["commit_absent_observed"] = False
        with self.assertRaisesRegex(ValueError, "guard_failed"): self.check(self.unknown(), copy.deepcopy(self.prepared))
        self.facts["commit_absent_observed"] = True
        self.facts["commit_observed"] = False
        with self.assertRaisesRegex(ValueError, "no_commit_attempt"): self.check(self.unknown("object_write"), copy.deepcopy(self.committed))
        rejected = copy.deepcopy(self.prepared); rejected.update(state="rejected", error={"code": "CANCELLED"})
        self.facts["attempt_stopped"] = False
        with self.assertRaisesRegex(ValueError, "guard_failed"): self.check(self.unknown(), rejected)

    def test_candidate_attempt_and_unknown_kind_are_fixed(self):
        r = copy.deepcopy(self.committed); r["candidate"]["object_version_ref"] = "different"
        with self.assertRaisesRegex(ValueError, "candidate_changed"): self.check(self.prepared, r)
        r = copy.deepcopy(self.committed); r["attempt"]["lease_owner_id"] = "stale-worker"
        with self.assertRaisesRegex(ValueError, "attempt_changed"): self.check(self.prepared, r)
        r = self.unknown("object_write"); r["candidate"] = copy.deepcopy(self.prepared["candidate"])
        with self.assertRaisesRegex(ValueError, "unknown_kind"): self.check(self.prepared, r)

    def test_retry_reset_requires_stop_and_preserved_history(self):
        for name in ("attempt_stopped", "history_preserved", "no_object_observed"):
            self.facts[name] = False
            reset = copy.deepcopy(self.accepted); reset["fencing_high_water"] = 4
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "guard_failed"):
                self.check(self.unknown("object_write"), reset)
            self.facts[name] = True

    def test_contradictory_commit_observations_rejected(self):
        self.facts["commit_absent_observed"] = True
        with self.assertRaisesRegex(ValueError, "contradictory_commit"):
            self.check(self.unknown(), copy.deepcopy(self.committed))


if __name__ == "__main__": unittest.main()

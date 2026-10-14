"""Offline transition design checker; supplied facts are not trusted runtime proof."""
from upload_operation_validation import validate_record, IDENTITY

FLAGS = ("actor_authorized", "hold_active", "writers_enabled", "stream_verified", "object_verified",
         "storage_epoch_matches", "draft_matches", "attempt_stopped", "no_object_observed",
         "commit_absent_observed", "commit_observed", "history_preserved")
EDGES = {("accepted", "receiving"), ("receiving", "validated"), ("validated", "materializing"),
         ("materializing", "prepared"), ("materializing", "reconciling"), ("prepared", "committed"),
         ("prepared", "reconciling"), ("reconciling", "prepared"), ("reconciling", "accepted"),
         ("reconciling", "committed")}
EDGES |= {(s, "rejected") for s in ("accepted", "receiving", "validated", "materializing", "prepared", "reconciling")}
EDGES |= {(s, "accepted") for s in ("receiving", "validated")}


def validate_transition(before, after, expected, facts):
    validate_record(before, expected); validate_record(after, expected)
    if not isinstance(facts, dict) or set(facts) != set(FLAGS) | {"now_seconds", "expected_row_version"}:
        raise ValueError("upload_transition_facts_required")
    if any(type(facts[k]) is not bool for k in FLAGS):
        raise ValueError("upload_transition_boolean_required")
    if any(type(facts[k]) is not int for k in ("now_seconds", "expected_row_version")) or not 0 <= facts["now_seconds"] <= 9223372036854775807:
        raise ValueError("upload_transition_integer_required")
    if facts["commit_observed"] and facts["commit_absent_observed"]:
        raise ValueError("upload_transition_contradictory_commit_observation")
    def require(*names):
        if any(not facts[k] for k in names): raise ValueError("upload_transition_guard_failed")
    if any(before[k] != after[k] for k in IDENTITY + ("retention_hold_id", "audit_reference")):
        raise ValueError("upload_transition_identity_changed")
    if before["row_version"] != facts["expected_row_version"] or after["row_version"] != before["row_version"] + 1:
        raise ValueError("upload_transition_row_version_mismatch")
    if before["cancel_requested"] and not after["cancel_requested"]:
        raise ValueError("upload_transition_cancel_cleared")
    source, target = before["state"], after["state"]
    edge = (source, target)
    # Same-state updates allow only setting the cancellation intent.
    if source == target:
        comparable = {k: v for k, v in after.items() if k not in {"row_version", "cancel_requested"}}
        baseline = {k: v for k, v in before.items() if k not in {"row_version", "cancel_requested"}}
        if source in {"committed", "rejected"} or comparable != baseline or before["cancel_requested"] or not after["cancel_requested"]:
            raise ValueError("upload_transition_invalid_edge")
        require("hold_active")
        return {"transition_checked": True, "ready_for_confirmation": False}
    if edge not in EDGES: raise ValueError("upload_transition_invalid_edge")
    require("hold_active")
    if after["fencing_high_water"] < before["fencing_high_water"]:
        raise ValueError("upload_transition_fencing_regressed")
    if source == "accepted" and target == "receiving":
        if after["fencing_high_water"] <= before["fencing_high_water"]:
            raise ValueError("upload_transition_fencing_not_new")
        if after["attempt"]["lease_until_seconds"] <= facts["now_seconds"]:
            raise ValueError("upload_transition_lease_expired")
        require("actor_authorized", "writers_enabled")
    else:
        if after["fencing_high_water"] != before["fencing_high_water"]:
            raise ValueError("upload_transition_fencing_changed")
        if target != "accepted" and before["attempt"] != after["attempt"]:
            raise ValueError("upload_transition_attempt_changed")
    if before["candidate"] is not None and target != "accepted" and before["candidate"] != after["candidate"]:
        raise ValueError("upload_transition_candidate_changed")
    if target == "validated": require("stream_verified")
    if edge == ("prepared", "reconciling") and after["unknown_result"] != "metadata_commit":
        raise ValueError("upload_transition_unknown_kind")
    if edge == ("materializing", "reconciling") and after["unknown_result"] != "object_write":
        raise ValueError("upload_transition_unknown_kind")
    if source == "reconciling":
        if before["unknown_result"] == "metadata_commit":
            if target == "committed": require("commit_observed")
            else: require("commit_absent_observed")
        elif target == "committed":
            raise ValueError("upload_transition_no_commit_attempt")
    if target == "accepted":
        require("attempt_stopped", "history_preserved")
        if source == "reconciling": require("no_object_observed")
    if target == "rejected" and before["attempt"] is not None:
        require("attempt_stopped", "history_preserved")
    # Recovery may record a previously committed transaction despite later cancellation.
    observed_commit = edge == ("reconciling", "committed")
    if target in {"receiving", "validated", "materializing", "prepared", "accepted", "committed"} and not observed_commit:
        if before["cancel_requested"] or after["cancel_requested"]:
            raise ValueError("upload_transition_cancel_pending")
    if target in {"validated", "materializing", "prepared", "committed"} and not observed_commit:
        if before["attempt"]["lease_until_seconds"] <= facts["now_seconds"]:
            raise ValueError("upload_transition_lease_expired")
        require("actor_authorized", "writers_enabled")
    if target in {"prepared", "committed"}: require("object_verified", "storage_epoch_matches")
    if edge == ("prepared", "committed"): require("draft_matches", "commit_observed")
    return {"transition_checked": True, "ready_for_confirmation": False}

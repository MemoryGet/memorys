"""Offline state transition rules; never pause writes, switch epochs, or release holds."""
from jsonschema import Draft202012Validator

from build_migration_state import build
from retention_validation import validate_policy

FORWARD=("planned","quiescing","building","verifying","ready","cutover_committed","restart_verified","serving_retained","finalized")
PRE_CUTOVER=set(FORWARD[:5])
POST_CUTOVER=set(FORWARD[5:8])
TERMINAL={"finalized","aborted","rolled_back"}
EDGES=set(zip(FORWARD,FORWARD[1:])) | {(s,"aborting") for s in PRE_CUTOVER} | {("aborting","aborted")}
EDGES |= {(s,"rollback_quiescing") for s in POST_CUTOVER}
EDGES |= set(zip(("rollback_quiescing","rollback_ready","rollback_committed","rollback_restart_verified"),
                 ("rollback_ready","rollback_committed","rollback_restart_verified","rolled_back")))
IMMUTABLE=("schema_version","kind","migration_id","space_id","kb_id","source_epoch_id","target_epoch_id",
           "source_lock_sha256","target_lock_sha256","target_index_profile_digest","journal_stream_id",
           "retention_hold_id","retention_policy_id","retention_policy_version")
FACT_FLAGS=("actor_authorized","scope_matches","writers_quiesced","required_artifacts_verified",
            "forward_checkpoint_verified","restart_binding_verified","rollback_checkpoint_verified",
            "no_pending_tasks","hold_active")


def validate_record(record,policy):
    Draft202012Validator(build()).validate(record);validate_policy(policy)
    for name,value in record.items():
        if name in ("schema_version","retention_policy_version","row_version","fencing_token") or name.endswith("_seconds"):
            if value is not None and type(value) is not int:raise ValueError("migration_integer_required")
    if record["source_epoch_id"]==record["target_epoch_id"]:raise ValueError("migration_identical_epochs")
    if (record["retention_policy_id"],record["retention_policy_version"])!=(policy["policy_id"],policy["policy_version"]):
        raise ValueError("migration_retention_policy_mismatch")
    phase=record["phase"]
    paused=phase not in {"planned","serving_retained","finalized","aborted","rolled_back"}
    if record["content_writes_paused"]!=paused:raise ValueError("migration_write_gate_mismatch")
    if phase in {"planned","quiescing"} and record["snapshot_cursor"] is not None:
        raise ValueError("migration_premature_snapshot")
    if phase in {"planned","quiescing","building"} and record["forward_checkpoint"] is not None:
        raise ValueError("migration_premature_checkpoint")
    if phase=="rollback_quiescing" and record["rollback_checkpoint"] is not None:
        raise ValueError("migration_premature_rollback_checkpoint")
    if phase in {"rollback_quiescing","rollback_ready"} and record["rollback_cutover_cursor"] is not None:
        raise ValueError("migration_premature_rollback_cutover")
    if phase in set(FORWARD[2:]) | {"rollback_quiescing","rollback_ready","rollback_committed","rollback_restart_verified","rolled_back"}:
        if record["snapshot_cursor"] is None:raise ValueError("migration_snapshot_required")
    if phase in set(FORWARD[4:]) | {"rollback_quiescing","rollback_ready","rollback_committed","rollback_restart_verified","rolled_back"}:
        if record["forward_checkpoint"] is None:raise ValueError("migration_forward_checkpoint_required")
    if phase in set(FORWARD[5:]) | {"rollback_quiescing","rollback_ready","rollback_committed","rollback_restart_verified","rolled_back"}:
        if record["cutover_cursor"] is None or record["cutover_cursor"]!=record["forward_checkpoint"]:
            raise ValueError("migration_cutover_checkpoint_mismatch")
    if phase in {"rollback_ready","rollback_committed","rollback_restart_verified","rolled_back"}:
        if record["rollback_checkpoint"] is None:raise ValueError("migration_rollback_checkpoint_required")
    if phase in {"rollback_committed","rollback_restart_verified","rolled_back"}:
        if record["rollback_cutover_cursor"] is None or record["rollback_cutover_cursor"]!=record["rollback_checkpoint"]:
            raise ValueError("migration_rollback_cutover_mismatch")
    times=[record[k] for k in ("resumed_at_seconds","rollback_until_seconds","retain_until_seconds")]
    if phase in PRE_CUTOVER|{"aborting","aborted"} and record["cutover_cursor"] is not None:
        raise ValueError("migration_premature_cutover")
    if not phase.startswith("rollback_") and phase!="rolled_back" and (record["rollback_checkpoint"] is not None or record["rollback_cutover_cursor"] is not None):
        raise ValueError("migration_premature_rollback")
    if times[0] is not None and phase in PRE_CUTOVER|{"aborting","aborted","cutover_committed","restart_verified"}:
        raise ValueError("migration_premature_resume")
    if any(t is not None for t in times) and any(t is None for t in times):raise ValueError("migration_partial_retention_window")
    if phase in {"serving_retained","finalized"} and times[0] is None:raise ValueError("migration_retention_window_required")
    if times[0] is not None:
        if times[1]<times[0]+policy["rollback_window_seconds"] or times[2]<times[1]+policy["replay_budget_seconds"]+policy["recovery_margin_seconds"]:
            raise ValueError("migration_retention_window_short")


def validate_transition(previous,candidate,facts,policy,now_seconds):
    validate_record(previous,policy);validate_record(candidate,policy)
    if type(now_seconds) is not int or not 0<=now_seconds<=253402300799:raise ValueError("migration_clock_invalid")
    if previous["resumed_at_seconds"] is not None and now_seconds<previous["resumed_at_seconds"]:
        raise ValueError("migration_clock_before_resume")
    if not isinstance(facts,dict) or set(facts)!=set(FACT_FLAGS)|{"row_version","fencing_token","active_epoch_id"}:
        raise ValueError("migration_independent_facts_required")
    if any(type(facts[key]) is not bool for key in FACT_FLAGS):raise ValueError("migration_fact_type_invalid")
    if any(type(facts[key]) is not int for key in ("row_version","fencing_token")):raise ValueError("migration_fact_type_invalid")
    if any(previous[k]!=candidate[k] for k in IMMUTABLE):raise ValueError("migration_identity_changed")
    if candidate["row_version"]!=previous["row_version"]+1 or facts["row_version"]!=previous["row_version"]:
        raise ValueError("migration_stale_row_version")
    if candidate["fencing_token"]!=previous["fencing_token"] or facts["fencing_token"]!=previous["fencing_token"]:
        raise ValueError("migration_stale_fence")
    source=previous["phase"];target=candidate["phase"]
    if (source,target) not in EDGES:raise ValueError("migration_transition_forbidden")
    if not facts["actor_authorized"] or not facts["scope_matches"]:raise ValueError("migration_authority_required")
    if not facts["hold_active"]:raise ValueError("migration_hold_required")
    # Facts are independently supplied model observations, not booleans to trust
    # from a production request. Compare source active epoch before transition.
    expected_epoch=previous["target_epoch_id"] if source in POST_CUTOVER|{"rollback_quiescing","rollback_ready"} else previous["source_epoch_id"]
    if facts["active_epoch_id"]!=expected_epoch:raise ValueError("migration_active_epoch_mismatch")
    for name in ("snapshot_cursor","cutover_cursor","resumed_at_seconds","rollback_until_seconds","retain_until_seconds","rollback_cutover_cursor"):
        if previous[name] is not None and candidate[name]!=previous[name]:raise ValueError("migration_frozen_checkpoint_changed")
    creation_phases={"snapshot_cursor":{"building"},"forward_checkpoint":{"verifying","ready","cutover_committed"},
                     "cutover_cursor":{"cutover_committed"},"rollback_checkpoint":{"rollback_ready","rollback_committed"},
                     "rollback_cutover_cursor":{"rollback_committed"},"resumed_at_seconds":{"serving_retained"},
                     "rollback_until_seconds":{"serving_retained"},"retain_until_seconds":{"serving_retained"}}
    for name,allowed in creation_phases.items():
        if previous[name]!=candidate[name] and target not in allowed:raise ValueError("migration_checkpoint_wrong_phase")
    if target in {"building","rollback_ready","cutover_committed","rollback_committed"} and not facts["writers_quiesced"]:
        raise ValueError("migration_writer_barrier_required")
    if target in {"ready","cutover_committed"} and not (facts["required_artifacts_verified"] and facts["forward_checkpoint_verified"]):
        raise ValueError("migration_forward_proof_required")
    if target in {"rollback_ready","rollback_committed"} and not facts["rollback_checkpoint_verified"]:
        raise ValueError("migration_rollback_proof_required")
    if target in {"restart_verified","rollback_restart_verified","serving_retained","rolled_back","aborted"} and not facts["restart_binding_verified"]:
        raise ValueError("migration_restart_proof_required")
    if target in {"aborted","finalized","rolled_back"} and not facts["no_pending_tasks"]:
        raise ValueError("migration_pending_tasks")
    if target=="serving_retained" and candidate["resumed_at_seconds"]!=now_seconds:
        raise ValueError("migration_resume_clock_mismatch")
    if target=="rollback_quiescing" and previous["rollback_until_seconds"] is not None and now_seconds>previous["rollback_until_seconds"]:
        raise ValueError("migration_rollback_window_expired")
    if target=="finalized" and now_seconds<candidate["retain_until_seconds"]:
        raise ValueError("migration_retention_still_active")
    return {"status":"offline_migration_transition_validated","ready_for_cutover":False,
            "unverified":["fact_provenance","actual_cas_and_fencing","current_authorization","write_barrier",
                          "proof_artifacts","atomic_epoch_switch","restart_identity","retention_hold_release"]}

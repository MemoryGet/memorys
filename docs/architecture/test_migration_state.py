"""State rules over synthetic facts; not runtime migration authorization."""
import copy
import json
from pathlib import Path
import unittest

from migration_state_validation import FACT_FLAGS,validate_record,validate_transition


class MigrationStateTests(unittest.TestCase):
    def setUp(self):
        self.policy=json.loads((Path(__file__).parent/"examples/design-retention-policy.json").read_text(encoding="utf-8"))
        self.record={"schema_version":1,"kind":"kb_backend","migration_id":"migration-1","space_id":"space-1","kb_id":"kb-1",
                     "source_epoch_id":"old-epoch","target_epoch_id":"new-epoch","source_lock_sha256":"a"*64,"target_lock_sha256":"b"*64,
                     "target_index_profile_digest":"c"*64,"journal_stream_id":"stream-1","retention_hold_id":"hold-1",
                     "retention_policy_id":self.policy["policy_id"],"retention_policy_version":1,"row_version":1,"fencing_token":3,
                     "phase":"planned","content_writes_paused":False,"snapshot_cursor":None,"forward_checkpoint":None,
                     "cutover_cursor":None,"rollback_checkpoint":None,"rollback_cutover_cursor":None,
                     "resumed_at_seconds":None,"rollback_until_seconds":None,"retain_until_seconds":None}
        self.now=1000000

    def candidate(self,target):
        new=copy.deepcopy(self.record);new["phase"]=target;new["row_version"]+=1
        new["content_writes_paused"]=target not in {"planned","serving_retained","finalized","aborted","rolled_back"}
        if target=="building":new["snapshot_cursor"]="j1:"+"0"*64
        if target=="ready":new["forward_checkpoint"]="j1:"+"1"*64
        if target=="cutover_committed":new["cutover_cursor"]=new["forward_checkpoint"]
        if target=="serving_retained":
            new["resumed_at_seconds"]=self.now
            new["rollback_until_seconds"]=self.now+self.policy["rollback_window_seconds"]
            new["retain_until_seconds"]=new["rollback_until_seconds"]+self.policy["replay_budget_seconds"]+self.policy["recovery_margin_seconds"]
        if target=="rollback_ready":new["rollback_checkpoint"]="j1:"+"2"*64
        if target=="rollback_committed":new["rollback_cutover_cursor"]=new["rollback_checkpoint"]
        return new

    def facts(self):
        new_epoch=self.record["phase"] in {"cutover_committed","restart_verified","serving_retained","rollback_quiescing","rollback_ready"}
        return {**{name:True for name in FACT_FLAGS},"row_version":self.record["row_version"],"fencing_token":self.record["fencing_token"],
                "active_epoch_id":self.record["target_epoch_id"] if new_epoch else self.record["source_epoch_id"]}

    def advance(self,target):
        new=self.candidate(target)
        report=validate_transition(self.record,new,self.facts(),self.policy,self.now)
        self.assertFalse(report["ready_for_cutover"]);self.record=new

    def through(self,target):
        for phase in ("quiescing","building","verifying","ready","cutover_committed","restart_verified","serving_retained"):
            self.advance(phase)
            if phase==target:return

    def test_forward_route_and_retention_finalize(self):
        self.through("serving_retained")
        self.now=self.record["retain_until_seconds"];self.advance("finalized")

    def test_pre_cutover_abort_and_post_cutover_rollback_routes(self):
        self.through("ready");self.advance("aborting");self.advance("aborted")
        self.setUp();self.through("serving_retained")
        for phase in ("rollback_quiescing","rollback_ready","rollback_committed","rollback_restart_verified","rolled_back"):
            self.advance(phase)

    def test_post_cutover_cannot_be_aborted_or_jump_to_serving(self):
        self.through("cutover_committed")
        attempted_abort=self.candidate("aborting");attempted_abort["cutover_cursor"]=None
        with self.assertRaisesRegex(ValueError,"transition_forbidden"):
            validate_transition(self.record,attempted_abort,self.facts(),self.policy,self.now)
        with self.assertRaisesRegex(ValueError,"transition_forbidden"):
            validate_transition(self.record,self.candidate("serving_retained"),self.facts(),self.policy,self.now)

    def test_late_worker_and_stale_row_cannot_transition(self):
        new=self.candidate("quiescing")
        for field in ("row_version","fencing_token"):
            facts=self.facts();facts[field]-=1
            with self.assertRaises(ValueError):validate_transition(self.record,new,facts,self.policy,self.now)

    def test_identity_epoch_hold_and_authority_mismatch_rejected(self):
        new=self.candidate("quiescing");new["kb_id"]="other"
        with self.assertRaisesRegex(ValueError,"identity_changed"):validate_transition(self.record,new,self.facts(),self.policy,self.now)
        new=self.candidate("quiescing")
        for key,value in (("hold_active",False),("scope_matches",False),("actor_authorized",False),("active_epoch_id","foreign")):
            facts=self.facts();facts[key]=value
            with self.assertRaises(ValueError):validate_transition(self.record,new,facts,self.policy,self.now)

    def test_cutover_and_restart_require_separate_proofs(self):
        self.through("ready");new=self.candidate("cutover_committed")
        for key in ("writers_quiesced","required_artifacts_verified","forward_checkpoint_verified"):
            facts=self.facts();facts[key]=False
            with self.assertRaises(ValueError):validate_transition(self.record,new,facts,self.policy,self.now)
        self.advance("cutover_committed");facts=self.facts();facts["restart_binding_verified"]=False
        with self.assertRaisesRegex(ValueError,"restart_proof_required"):
            validate_transition(self.record,self.candidate("restart_verified"),facts,self.policy,self.now)

    def test_resume_window_is_actual_time_and_cannot_be_shortened(self):
        self.through("restart_verified");new=self.candidate("serving_retained");new["resumed_at_seconds"]-=1
        with self.assertRaisesRegex(ValueError,"resume_clock_mismatch"):validate_transition(self.record,new,self.facts(),self.policy,self.now)
        self.advance("serving_retained");new=self.candidate("rollback_quiescing");new["retain_until_seconds"]-=1
        with self.assertRaises(ValueError):validate_transition(self.record,new,self.facts(),self.policy,self.now)

    def test_expired_rollback_and_early_finalization_rejected(self):
        self.through("serving_retained")
        with self.assertRaisesRegex(ValueError,"retention_still_active"):
            validate_transition(self.record,self.candidate("finalized"),self.facts(),self.policy,self.now)
        self.now=self.record["rollback_until_seconds"]+1
        with self.assertRaisesRegex(ValueError,"rollback_window_expired"):
            validate_transition(self.record,self.candidate("rollback_quiescing"),self.facts(),self.policy,self.now)

    def test_missing_checkpoint_or_wrong_phase_injection_rejected(self):
        new=self.candidate("quiescing");new["snapshot_cursor"]="j1:"+"0"*64
        with self.assertRaisesRegex(ValueError,"premature_snapshot"):validate_transition(self.record,new,self.facts(),self.policy,self.now)
        self.through("ready");new=self.candidate("cutover_committed");new["cutover_cursor"]="j1:"+"9"*64
        with self.assertRaisesRegex(ValueError,"cutover_checkpoint_mismatch"):validate_transition(self.record,new,self.facts(),self.policy,self.now)

    def test_false_string_proof_and_float_counter_rejected(self):
        facts=self.facts();facts["hold_active"]="false"
        with self.assertRaisesRegex(ValueError,"fact_type_invalid"):
            validate_transition(self.record,self.candidate("quiescing"),facts,self.policy,self.now)
        new=self.candidate("quiescing");new["row_version"]=2.0
        with self.assertRaisesRegex(ValueError,"integer_required"):validate_record(new,self.policy)

    def test_clock_regression_and_missing_cleanup_barrier_rejected(self):
        self.through("serving_retained")
        with self.assertRaisesRegex(ValueError,"clock_before_resume"):
            validate_transition(self.record,self.candidate("rollback_quiescing"),self.facts(),self.policy,self.now-1)
        self.now=self.record["retain_until_seconds"];facts=self.facts();facts["no_pending_tasks"]=False
        with self.assertRaisesRegex(ValueError,"pending_tasks"):
            validate_transition(self.record,self.candidate("finalized"),facts,self.policy,self.now)

    def test_rollback_before_resume_still_requires_checkpoint_and_restart(self):
        self.through("cutover_committed");self.advance("rollback_quiescing")
        facts=self.facts();facts["rollback_checkpoint_verified"]=False
        with self.assertRaisesRegex(ValueError,"rollback_proof_required"):
            validate_transition(self.record,self.candidate("rollback_ready"),facts,self.policy,self.now)
        for phase in ("rollback_ready","rollback_committed","rollback_restart_verified","rolled_back"):
            self.advance(phase)
        self.assertIsNone(self.record["resumed_at_seconds"])

    def test_started_rollback_remains_protected_after_window_end(self):
        self.through("serving_retained");self.advance("rollback_quiescing")
        self.now=self.record["retain_until_seconds"]+1
        self.advance("rollback_ready")
        facts=self.facts();facts["hold_active"]=False
        with self.assertRaisesRegex(ValueError,"hold_required"):
            validate_transition(self.record,self.candidate("rollback_committed"),facts,self.policy,self.now)


if __name__=="__main__":unittest.main()

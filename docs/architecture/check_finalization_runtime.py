"""Actual atomic publication results, independent command hash and public DTOs."""
import copy
import hashlib
import json
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from check_architecture import source_inventory
from check_preparation_runtime import encoded

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"finalization_actual_completed_index", "finalization_atomic_history_head", "finalization_cold_unknown_replay", "finalization_semantic_conflict", "finalization_current_publisher_authority", "finalization_replacement_cas_single_winner", "finalization_old_publication_preserved_on_failure", "finalization_fixed_old_cleanup_isolation", "finalization_withdrawal_interoperation", "finalization_replay_after_withdrawal_no_resurrection", "finalization_deleted_index_revoked_approval", "finalization_audit_outbox_atomicity", "finalization_deferred_event_and_head_gates", "finalization_precommit_revocation", "finalization_readiness_write_barrier", "finalization_configuration_superseded"}


def validate():
    sources = source_inventory()
    spec = json.loads((ROOT / "docs/architecture/openapi/management.v1.json").read_text(encoding="utf-8"))
    rows = []
    for major in (15, 16, 18):
        report = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete finalization evidence")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])):
            raise ValueError("missing actual publication scenarios")
        results, requests, projections = [proof.get(k, []) for k in ("finalization_results", "finalization_requests", "finalization_contract_samples")]
        if len(results) != 2 or len(requests) != 2 or len(projections) != 4:
            raise ValueError("missing first/replacement publication samples")
        for i, (result, request) in enumerate(zip(results, requests)):
            command = request["Command"]
            actor = copy.deepcopy(command["Principal"])
            if actor.get("Delegation") is not None:
                actor["Delegation"]["ProofReference"] = ""
            semantic = {"format": "akos.publication-finalize/1", "space_id": command["Scope"]["SpaceID"], "kb_id": command["Scope"]["KnowledgeBaseIDs"][0], "actor": actor, "operation_id": request["OperationID"], "expected_epoch": request["ExpectedEpoch"], "artifact_digest": request["ArtifactDigest"], "profile_digest": command["Request"]["ProfileDigest"]}
            digest = hashlib.sha256(b"akos.publication-finalize/1\x00" + encoded(semantic)).hexdigest()
            h, o = result["History"], result["Operation"]
            if h["CommandDigest"] != digest or h["OperationID"] != request["OperationID"] or o["ID"] != request["OperationID"] or o["State"] != "committed" or o["DocumentID"] != h["DocumentID"] or o["RevisionID"] != h["RevisionID"] or o["GenerationSetID"] != h["GenerationSetID"] or h["Action"] != "publish" or h["ActorID"] != command["Principal"]["SubjectID"] or h["Actor"] != command["Principal"] or h["PreviousEpoch"] != request["ExpectedEpoch"] or h["PublicationEpoch"] != h["PreviousEpoch"] + 1 or o["ExpectedEpoch"] != h["PreviousEpoch"]:
                raise ValueError("publication operation/history/semantic binding differs")
            public_o, public_h = projections[2*i], projections[2*i+1]
            if public_o["schema"] != "PublicationOperation" or public_h["schema"] != "PublicationHistory":
                raise ValueError("public projection order differs")
            for public, native, mapping in ((public_o["payload"], o, {"operation_id": "ID", "document_id": "DocumentID", "revision_id": "RevisionID", "approval_id": "ApprovalID", "expected_epoch": "ExpectedEpoch", "state": "State", "generation_set_id": "GenerationSetID"}), (public_h["payload"], h, {"id": "ID", "document_id": "DocumentID", "publication_epoch": "PublicationEpoch", "revision_id": "RevisionID", "action": "Action", "actor_id": "ActorID", "reason": "Reason", "occurred_at": "OccurredAt"})):
                if any(public[k] != native[v] for k, v in mapping.items()):
                    raise ValueError("public projection does not match persisted native result")
        first, second = results
        if first["History"]["PublicationEpoch"] != 1 or first["CleanupPlanID"] is not None or first["History"]["PreviousRevisionID"] is not None or first["History"]["PreviousGenerationSetID"] is not None or second["History"]["PublicationEpoch"] != 2 or not second["CleanupPlanID"] or second["History"]["PreviousRevisionID"] != first["History"]["RevisionID"] or second["History"]["PreviousGenerationSetID"] != first["History"]["GenerationSetID"] or second["History"]["GenerationSetID"] == first["History"]["GenerationSetID"] or second["DocumentRowVersion"] != first["DocumentRowVersion"] + 1:
            raise ValueError("replacement lost previous source/CAS binding")
        for projection in projections:
            name = projection["schema"]
            if name not in {"PublicationOperation", "PublicationHistory"}:
                raise ValueError("unexpected public publication projection")
            Draft202012Validator({"$ref": f"#/components/schemas/{name}", "components": spec["components"]}, format_checker=FormatChecker()).validate(projection["payload"])
        rows.append({"major": major, "server_version": report["server_version"], "publication_results": 2, "public_projections": 4, "command_commitments": 2, "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Actual first/replacement publication CAS, fixed old-source cleanup and normal withdrawal interoperability; independent command commitments and public Schema; historical results are not current authority", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["published retriever and final evidence output checks", "changed-content revision replacement and external engines", "production IAM/RLS, whole metadata UoW/migrations/rollback", "continuous workers and complete cleanup pipeline", "public REST/MCP/UI, sealed Chinese recall and performance"]}


def main():
    path = ROOT / ".local/architecture-check/finalization-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Finalization: 6 actual publication results, 12 public DTOs and 6 independent commitments passed across PG15/16/18")


if __name__ == "__main__":
    main()

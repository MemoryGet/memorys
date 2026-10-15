"""Actual preparation completion results: independent Job Schema and bindings."""
import copy
import hashlib
import json
from datetime import datetime
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from check_architecture import source_inventory
from check_preparation_runtime import encoded

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"completion_actual_projection_reinspection", "completion_immutable_success_closes_lease", "completion_cold_unknown_replay_after_expiry", "completion_exact_command_binding", "completion_expired_takeover_denied", "completion_deleted_projection_rejected", "completion_replay_worker_publisher_authority", "completion_precommit_revocation", "completion_precommit_expiry", "completion_configuration_superseded", "completion_effects_atomicity", "completion_deferred_effects_and_head_gate", "completion_concurrent_idempotency", "completion_no_publication_cutover"}


def validate():
    sources = source_inventory()
    spec = json.loads((ROOT / "docs/architecture/openapi/management.v1.json").read_text(encoding="utf-8"))
    validator = Draft202012Validator({"$ref": "#/components/schemas/Job", "components": spec["components"]}, format_checker=FormatChecker())
    rows = []
    for major in (15, 16, 18):
        report = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete completion evidence")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])):
            raise ValueError("missing actual completion scenarios")
        results, jobs = proof.get("completion_samples", []), proof.get("completion_job_samples", [])
        if len(results) != 2 or len(jobs) != 2:
            raise ValueError("missing completed or renewed completion samples")
        for result, projection in zip(results, jobs):
            if projection["schema"] != "Job":
                raise ValueError("unexpected completion projection")
            validator.validate(projection["payload"])
            job, artifact = result["Job"], result["Artifact"]
            envelope = copy.deepcopy(artifact)
            digest = envelope.pop("Digest")
            if digest != hashlib.sha256(b"akos.prepared-artifact/1\x00" + encoded({"format": "akos.prepared-artifact/1", "artifact": envelope})).hexdigest():
                raise ValueError("completed artifact commitment differs")
            generation = artifact["GenerationSet"]["Channels"]["keyword"]
            if job["State"] != "completed" or job["Progress"] != 1 or job["LeaseUntil"] is not None or job["Error"] is not None or job["RowVersion"] < 3 or job["FencingToken"] != artifact["FencingToken"] or job["ProfileDigest"] != artifact["Profile"]["Digest"] or job["OperationID"] != artifact["OperationID"] or job["ID"] != artifact["JobID"] or job["GenerationID"] != generation["ID"] or job["RevisionID"] != artifact["GenerationSet"]["RevisionID"]:
                raise ValueError("completion lost artifact/task binding")
            payload = projection["payload"]
            for wire, native in {"id": "ID", "space_id": "SpaceID", "resource_id": "OperationID", "state": "State", "stage": "Stage", "attempt": "Attempt", "row_version": "RowVersion", "progress": "Progress"}.items():
                if payload[wire] != job[native]:
                    raise ValueError("completion Job projection differs")
            if datetime.fromisoformat(result["CompletedAt"].replace("Z", "+00:00")).utcoffset().total_seconds() != 0:
                raise ValueError("completion time is not UTC")
        if [r["Job"]["RowVersion"] for r in results] != [3, 4]:
            raise ValueError("claim/renew/completion version sequence differs")
        rows.append({"major": major, "server_version": report["server_version"], "completion_results": 2, "job_projections": 2, "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Actual atomic preparation completion and historical cold replay, independent artifact/task binding and Job Schema; no final publication", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["final publication CAS/current proof and retrieval", "continuous queue/task cancel/retry and artifact cleanup", "external engines and second completion backend replacement", "production IAM/RLS, migrations, API/MCP/UI, recall and performance"]}


def main():
    path = ROOT / ".local/architecture-check/completion-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Preparation completion: 6 actual results/Job projections passed independent binding and Schema across PG15/16/18")


if __name__ == "__main__":
    main()

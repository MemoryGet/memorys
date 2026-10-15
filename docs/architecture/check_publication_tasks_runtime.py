"""Actual publication task jobs and lease relationships; no index execution claim."""
import json
from datetime import datetime
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {
    "publication_tasks_durable_claim_replay", "publication_tasks_renewal_unknown_replay",
    "publication_tasks_expiry_takeover_fence", "publication_tasks_owned_frozen_content",
    "publication_tasks_worker_and_publisher_authority", "publication_tasks_precommit_revocation",
    "publication_tasks_atomic_audit_outbox", "publication_tasks_deferred_event_gate",
    "publication_tasks_concurrent_claim", "publication_tasks_configuration_superseded",
    "publication_tasks_review_epoch_superseded", "publication_tasks_no_publication",
    "publication_tasks_receipt_head_binding",
}


def validate():
    sources = source_inventory()
    management = json.loads((ROOT / "docs/architecture/openapi/management.v1.json").read_text(encoding="utf-8"))
    rows = []
    for major in (15, 16, 18):
        report = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete publication-task proof")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])):
            raise ValueError("missing task ownership scenarios")
        jobs = proof.get("publication_task_samples", [])
        if len(jobs) != 7:
            raise ValueError("missing actual running/superseded task projections")
        validator = Draft202012Validator({"$ref": "#/components/schemas/Job", "components": management["components"]}, format_checker=FormatChecker())
        for sample in jobs:
            if sample["schema"] != "Job":
                raise ValueError("unknown task projection")
            validator.validate(sample["payload"])
        if [j["payload"]["state"] for j in jobs] != ["running"] * 3 + ["failed"] * 4:
            raise ValueError("task state coverage differs")
        if [j["payload"]["attempt"] for j in jobs[:3]] != [1, 1, 2]:
            raise ValueError("claim/renew/takeover counters differ")
        leases = proof.get("publication_lease_samples", [])
        if len(leases) != 4:
            raise ValueError("missing actual lease relationships")
        for result in leases:
            lease = result["Lease"]
            if result["JobVersion"] < 2 or lease["FencingToken"] < 1 or any(not lease[k] for k in ("JobID", "WorkerID", "AttemptID")):
                raise ValueError("empty ownership data")
            if datetime.fromisoformat(lease["ExpiresAt"].replace("Z", "+00:00")).utcoffset().total_seconds() != 0:
                raise ValueError("lease deadline is not UTC")
        first, renewed, expired, takeover = [r["Lease"] for r in leases]
        if any(first[k] != renewed[k] for k in ("JobID", "WorkerID", "AttemptID", "FencingToken")) or renewed["ExpiresAt"] <= first["ExpiresAt"]:
            raise ValueError("renewal changed owner or failed to extend")
        if expired["JobID"] != takeover["JobID"] or expired["WorkerID"] == takeover["WorkerID"] or expired["AttemptID"] == takeover["AttemptID"] or takeover["FencingToken"] != expired["FencingToken"] + 1:
            raise ValueError("takeover did not fence the old attempt")
        rows.append({"major": major, "server_version": report["server_version"], "job_projections": 7, "lease_samples": 4, "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("sources or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Real lease/control-source/owned-content checks and independent Job Schema/lease relationships; not running queue/index worker", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["production worker/publisher authority and delegation", "external write fencing/attempt cleanup and continuous queue", "index prepare/readiness/completion/final publication", "full TaskRepository cancel/retry/UoW/migration", "public API/MCP and recall/performance"]}


def main():
    path = ROOT / ".local/architecture-check/publication-tasks-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Publication tasks: 21 actual Job projections and 12 lease samples passed across PG15/16/18")


if __name__ == "__main__":
    main()

"""Actual publication-start projections and rule digests; no index/cutover claim."""
import hashlib
import json
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from canonical_content import canonical
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {
    "publication_start_atomic_task_outbox", "publication_start_current_approval_policy",
    "publication_start_backend_binding", "publication_start_cold_replay",
    "publication_start_authority_recheck", "publication_start_effects_rollback",
    "publication_start_deferred_event_gate", "publication_start_head_write_fences",
    "publication_start_concurrent_key", "publication_start_no_head_cutover",
}


def validate():
    sources = source_inventory()
    management = json.loads((ROOT / "docs/architecture/openapi/management.v1.json").read_text(encoding="utf-8"))
    rows = []
    for major in (15, 16, 18):
        report = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete publication-start proof")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])):
            raise ValueError("missing publication-start scenarios")
        samples = proof.get("publication_contract_samples", [])
        if len(samples) != 6:
            raise ValueError("missing actual operation/job projection pairs")
        for index in range(0, len(samples), 2):
            operation, job = samples[index:index + 2]
            for sample in (operation, job):
                if sample["schema"] not in ("PublicationOperation", "Job"):
                    raise ValueError("unknown publication projection")
                schema = {"$ref": f"#/components/schemas/{sample['schema']}", "components": management["components"]}
                Draft202012Validator(schema, format_checker=FormatChecker()).validate(sample["payload"])
            op, task = operation["payload"], job["payload"]
            if operation["schema"] != "PublicationOperation" or job["schema"] != "Job" or op["state"] != "preparing" or op["generation_set_id"] is not None or task["state"] != "queued" or task["stage"] != "publication.prepare" or task["id"] != op["job_id"] or task["resource_id"] != op["operation_id"]:
                raise ValueError("preparation pair claims a different lifecycle state")
        policies = proof.get("publication_policy_samples", [])
        if len(policies) != 3 or [p["Version"] for p in policies] != [7, 8, 9]:
            raise ValueError("missing current-policy versions")
        for policy in policies:
            payload = {"format": "akos.review-policy/1", "version": policy["Version"], "accepted_decision_policy_versions": policy["AcceptedDecisionPolicyVersions"]}
            encoded = canonical(json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
            digest = hashlib.sha256(b"akos.review-policy/1\x00" + encoded).hexdigest()
            if digest != policy["Digest"]:
                raise ValueError("Go/Python review-policy commitment differs")
        rows.append({"major": major, "server_version": report["server_version"], "projections": 6, "policy_digests": 3, "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source/probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Actual preparation registrations: management Schema and independent review-policy digests; no index execution or publication cutover", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["policy/backend administrative authority and migration", "full task claims/fences/index readiness/final publication transaction", "public endpoints and production IAM", "published retrieval and Recall@10"]}


def main():
    path = ROOT / ".local/architecture-check/publication-start-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Publication start: 18 actual projections and 9 cross-language policy commitments passed across PG15/16/18")


if __name__ == "__main__":
    main()

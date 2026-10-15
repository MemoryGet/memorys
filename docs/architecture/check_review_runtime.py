"""Validate actual PG review projections against the management OpenAPI Schema."""
import json
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {
    "review_atomic_approve_reject", "review_immutable_revoke",
    "review_lost_response_cold_replay", "review_idempotency_and_authority",
    "review_failed_effects_rollback", "review_deferred_effect_gate",
    "review_precommit_authority", "review_concurrent_single_decision",
    "review_no_publication",
}


def validate():
    sources = source_inventory()
    document = json.loads((ROOT / "docs/architecture/openapi/management.v1.json").read_text(encoding="utf-8"))
    rows = []
    for major in (15, 16, 18):
        path = ROOT / f".local/architecture-check/postgres{major}-wsl.json"
        report = json.loads(path.read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError(f"stale or incomplete PG{major} proof")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])):
            raise ValueError("missing review transaction scenarios")
        samples = proof.get("review_contract_samples", [])
        if len(samples) != 7:
            raise ValueError("expected actual initial/current requests and three decisions")
        states, decisions = set(), set()
        for sample in samples:
            kind = sample["schema"]
            if kind not in ("ApprovalRequest", "Approval"):
                raise ValueError("unknown review projection")
            schema = {"$ref": f"#/components/schemas/{kind}", "components": document["components"]}
            Draft202012Validator(schema, format_checker=FormatChecker()).validate(sample["payload"])
            payload = sample["payload"]
            if kind == "ApprovalRequest":
                states.add(payload["state"])
                expected = {"submitted": 0, "approved": 1, "rejected": 1, "revoked": 2}[payload["state"]]
                if payload["row_version"] != expected:
                    raise ValueError("review request initial/transition version mismatch")
            else:
                decisions.add(payload["decision"])
                if payload["row_version"] != (2 if payload["decision"] == "revoke" else 1):
                    raise ValueError("decision version mismatch")
        if states != {"submitted", "approved", "rejected", "revoked"} or decisions != {"approve", "reject", "revoke"}:
            raise ValueError("incomplete review state coverage")
        rows.append({"major": major, "server_version": report["server_version"], "projections": len(samples), "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Actual PostgreSQL review verification projections against management OpenAPI; not public HTTP/MCP execution", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["real IAM/RLS and human delegation authority", "public endpoints and transport field mapping", "current-policy approval validity and publication", "full metadata UoW and migration/rollback"]}


def main():
    path = ROOT / ".local/architecture-check/review-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Review runtime: 21 actual projections passed management Schema and transition checks across PG15/16/18")


if __name__ == "__main__":
    main()

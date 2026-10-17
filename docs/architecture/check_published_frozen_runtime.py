"""Independently validate actual published frozen bytes, schema and both hashes."""
import base64
import json
from pathlib import Path

from canonical_content import canonical, frozen
from check_architecture import source_inventory
from check_frozen_content import validate_payload

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"frozen_published_" + name for name in (
    "actual_bytes", "typed_revision_reconstruction", "dual_hashes", "return_ownership",
    "scope_revision_draft_denial", "current_authority_recheck", "cancel_deadline", "withdrawn_revoked_denial")}


def validate():
    sources, rows = source_inventory(), []
    for major in (15, 16, 18):
        r = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (r.get("passed") and r.get("requested_major") == major and r.get("sources") == sources and r.get("sources_unchanged_during_checks") and r.get("server_stopped") and r.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete frozen-read evidence")
        proof = r["go_repository_proof"]
        samples = proof.get("published_frozen_samples", [])
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])) or len(samples) != 4:
            raise ValueError("missing actual frozen-read scenarios")
        facts = {f["DocumentID"]: f for f in proof["query_read_samples"][0]["facts"]}
        seen = set()
        for sample in samples:
            q, record, expected = [sample[k] for k in ("request", "record", "expected")]
            if record["DocumentID"] in seen or record["DocumentID"] != q["DocumentID"] or record["RevisionID"] != q["RevisionID"] or record["DocumentID"] != expected["DocumentID"] or record["RevisionID"] != expected["ID"]:
                raise ValueError("frozen platform identity differs")
            seen.add(record["DocumentID"])
            payload = base64.b64decode(record["Payload"], validate=True)
            if not payload or len(payload) > 4 << 20 or canonical(payload) != payload:
                raise ValueError("frozen bytes are not bounded canonical JSON")
            content = json.loads(payload)
            validate_payload(content)
            hashes = frozen(payload)
            fact = facts[record["DocumentID"]]
            if hashes != {"format": expected["HashFormat"], "blocks_hash": expected["BlocksHash"], "content_hash": expected["ContentHash"]} or fact["ContentDigest"] != expected["ContentHash"] or fact["RevisionID"] != record["RevisionID"] or q["Scope"]["SpaceID"] != fact["SpaceID"] or fact["KnowledgeBaseID"] not in q["Scope"]["KnowledgeBaseIDs"]:
                raise ValueError("independent frozen hash/publication binding differs")
        if seen != set(facts):
            raise ValueError("frozen sample inventory differs from actual publications")
        rows.append({"major": major, "server_version": r["server_version"], "actual_frozen_reads": len(samples), "independent_dual_hashes": len(samples), "go_executable_sha256": r["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Real current-published native Revision reconstruction and immutable canonical frozen reads; independent schema and dual hashes; current authorization callback is fixture", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["original/media bytes and evidence locators", "actual CurrentBackend/CurrentControl and final model/API output checks", "production IAM/RLS and query authority/Grant", "whole metadata migration/restart/rollback", "REST/MCP/UI, hybrid recall and performance"]}


def main():
    path = ROOT / ".local/architecture-check/published-frozen-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Published frozen reads: 12 actual canonical payloads passed independent schema and both hashes on PG15/16/18")


if __name__ == "__main__":
    main()

"""Actual SQL candidates: independent projection hash, row/citation and wire checks."""
import hashlib
import json
import math
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from check_architecture import source_inventory
from check_preparation_runtime import encoded

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"keyword_" + name for name in (
    "actual_published_sql", "platform_citations", "draft_unpublished_isolation", "projection_binding",
    "scope_route_generation_filters", "literal_query_no_sql", "deterministic_ranking_limit",
    "current_authority_failure", "final_authority_recheck", "cancellation", "withdrawn_source_rejected",
    "revoked_approval_rejected", "deleted_projection_rejected", "wire_schema")}


def check_sample(s, spec):
    q, projection, candidates, records = [s[k] for k in ("request", "projection", "candidates", "actual_rows")]
    digest = hashlib.sha256(b"akos.publication-projection/1\x00" + encoded(projection)).hexdigest()
    if q["Publication"]["SnapshotDigest"] != digest or projection["space_id"] != q["Scope"]["SpaceID"] or projection["knowledge_base_id"] != q["Scope"]["KnowledgeBaseIDs"][0] or projection["route_id"] != q["RouteID"] or projection["backend_epoch_id"] != q["BackendEpochID"] or projection["pipeline_digest"] != q["Request"]["ProfileDigest"]:
        raise ValueError("independent publication projection binding differs")
    entries = {e["generation_id"]: e for e in projection["entries"]}
    if sorted(entries) != q["Publication"]["GenerationIDs"] or sorted({e["revision_id"] for e in entries.values()}) != q["Publication"]["RevisionIDs"] or len(entries) < 3:
        raise ValueError("request/projection inventory differs")
    indexed = {}
    for record in records:
        key = (record["GenerationID"], record["Chunk"]["ID"])
        e = entries.get(record["GenerationID"])
        if key in indexed or not e or record["Published"] is not False or record["DocumentID"] != e["document_id"] or record["Chunk"]["RevisionID"] != e["revision_id"] or record["SpaceID"] != q["Scope"]["SpaceID"] or record["KnowledgeBaseID"] != "kb" or record["BackendEpochID"] != q["BackendEpochID"]:
            raise ValueError("actual immutable source rows differ")
        indexed[key] = record
    if not candidates or len(candidates) > q["Limit"]:
        raise ValueError("missing or excessive actual SQL candidates")
    previous, seen = None, set()
    for rank, candidate in enumerate(candidates, 1):
        if len(candidate["GenerationIDs"]) != 1:
            raise ValueError("raw keyword candidate has multiple generations")
        key = (candidate["GenerationIDs"][0], candidate["ChunkID"])
        record = indexed.get(key)
        if key in seen or record is None or candidate["Citation"] != record["Citation"] or candidate["Text"] != record["Chunk"]["Text"] or candidate["KnowledgeBaseID"] != record["KnowledgeBaseID"] or candidate["Rank"] != rank or candidate["RouteID"] != q["RouteID"] or candidate["RetrieverID"] != projection["module_instance_id"] or candidate["ScoreKind"] != "postgres_keyword_rank" or not math.isfinite(candidate["Score"]) or candidate["Score"] < 0:
            raise ValueError("SQL candidate is not a scoped actual source row")
        order = (-candidate["Score"], *key)
        if previous is not None and order < previous:
            raise ValueError("SQL deterministic ranking differs")
        previous = order
        seen.add(key)
    request_wire, result_wire = s["request_wire"], s["result_wire"]
    for name, payload in (("RetrieveInput", request_wire["input"]), ("RetrieveResult", result_wire["result"])):
        Draft202012Validator({"$ref": "#/components/schemas/" + name, "components": spec["components"]}, format_checker=FormatChecker()).validate(payload)
    if request_wire["context"]["request_id"] != q["Request"]["RequestID"] or request_wire["context"]["module_instance_id"] != projection["module_instance_id"] or request_wire["context"]["invocation_id"] != projection["invocation_id"] or result_wire["request_id"] != q["Request"]["RequestID"] or result_wire["invocation_id"] != projection["invocation_id"] or request_wire["input"]["publication"]["snapshot_digest"] != digest:
        raise ValueError("actual wire invocation differs")
    wire_candidates = result_wire["result"]["candidates"]
    if len(wire_candidates) != len(candidates):
        raise ValueError("actual wire candidate count differs")
    for wire, native in zip(wire_candidates, candidates):
        mapping = {"chunk_id": "ChunkID", "generation_ids": "GenerationIDs", "kb_id": "KnowledgeBaseID", "rank": "Rank", "route_id": "RouteID", "retriever_id": "RetrieverID", "score": "Score", "score_kind": "ScoreKind", "text": "Text"}
        if any(wire[k] != native[v] for k, v in mapping.items()):
            raise ValueError("wire candidate differs from actual SQL result")
        citation = native["Citation"]
        if any(wire["citation"][k] != citation[v] for k, v in {"document_id": "DocumentID", "revision_id": "RevisionID", "block_id": "BlockID"}.items()):
            raise ValueError("wire platform citation differs")
        # Current native parser fixtures explicitly provide original/coarse
        # anchors. More source forms require their own real sample coverage.
        source = citation["Source"]
        if source["Kind"] != "original" or source["Locator"]["Precision"] != "coarse" or wire["citation"]["source_anchor"] != {"kind": "original", "blob_id": source["BlobID"], "locator": {"precision": "coarse"}}:
            raise ValueError("actual coarse original citation changed")


def validate():
    sources, rows = source_inventory(), []
    spec = json.loads((ROOT / "docs/architecture/openapi/modules.v1.json").read_text(encoding="utf-8"))
    for major in (15, 16, 18):
        r = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (r.get("passed") and r.get("requested_major") == major and r.get("sources") == sources and r.get("sources_unchanged_during_checks") and r.get("server_stopped") and r.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete keyword evidence")
        p = r["go_repository_proof"]
        if not p.get("passed") or not REQUIRED.issubset(p.get("checks", [])) or len(p.get("keyword_retrieval_samples", [])) != 1:
            raise ValueError("missing actual keyword search scenarios")
        sample = p["keyword_retrieval_samples"][0]
        check_sample(sample, spec)
        rows.append({"major": major, "server_version": r["server_version"], "actual_candidates": len(sample["candidates"]), "actual_rows": len(sample["actual_rows"]), "go_executable_sha256": r["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Real native published keyword SQL, independent projection commitment, actual row/citation/ranking and module Schema; current permission/projection provider is fixture", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["production IAM and parent/child query authority/Grant ledger", "final evidence/current authority checks before model/API output", "hybrid/vector recall, second real retriever and replacement", "large dataset latency/concurrency, Chinese Recall@10 >=90%", "stateful migrations/rollback and production REST/MCP/UI"]}


def main():
    path = ROOT / ".local/architecture-check/keyword-retrieval-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Keyword retrieval: actual SQL candidates, source rows, projection commitments and module Schema passed on PG15/16/18")


if __name__ == "__main__":
    main()

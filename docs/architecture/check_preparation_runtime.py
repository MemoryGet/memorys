"""Actual PG preparation snapshots, standard proof DTOs and independent hashes."""
import copy
import hashlib
import json
from pathlib import Path

from jsonschema import Draft202012Validator
from canonical_content import canonical
from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"preparation_actual_keyword_projection", "preparation_durable_cold_replay", "preparation_independent_live_inspection", "preparation_scoped_query_filters", "preparation_sealed_rows_and_delete_fence", "preparation_rejects_echo_and_source_mutation", "preparation_audit_outbox_atomicity", "preparation_deferred_event_gate", "preparation_current_authority_and_lease_fence", "preparation_attempt_takeover_isolation", "preparation_concurrent_idempotency", "preparation_no_publication_cutover"}


def encoded(value):
    return canonical(json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))


def validate():
    sources = source_inventory()
    modules = json.loads((ROOT / "docs/architecture/openapi/modules.v1.json").read_text(encoding="utf-8"))
    validator = Draft202012Validator({"$ref": "#/components/schemas/PrepareResult", "components": modules["components"]})
    rows = []
    for major in (15, 16, 18):
        report = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete preparation proof")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])):
            raise ValueError("missing actual preparation scenarios")
        artifacts, snapshots, wires = [proof.get(k, []) for k in ("preparation_artifacts", "preparation_snapshots", "preparation_wire_samples")]
        if any(len(items) != 2 for items in (artifacts, snapshots, wires)):
            raise ValueError("missing actual preparation samples")
        for artifact, snapshot, wire in zip(artifacts, snapshots, wires):
            validator.validate(wire["result"])
            profile = copy.deepcopy(artifact["Profile"])
            profile_digest = profile.pop("Digest")
            expected = hashlib.sha256(b"akos.index-profile/1\x00" + encoded({"format": "akos.index-profile/1", "profile": profile})).hexdigest()
            if profile_digest != expected:
                raise ValueError("full profile commitment differs")
            envelope = copy.deepcopy(artifact)
            actual_digest = envelope.pop("Digest")
            if actual_digest != hashlib.sha256(b"akos.prepared-artifact/1\x00" + encoded({"format": "akos.prepared-artifact/1", "artifact": envelope})).hexdigest():
                raise ValueError("artifact commitment differs")
            body = copy.deepcopy(snapshot)
            checksum = body["Checksum"]
            body["Checksum"] = ""
            if checksum != hashlib.sha256(b"akos.index.snapshot/1\n" + encoded(body)).hexdigest() or checksum != artifact["SnapshotDigest"]:
                raise ValueError("actual row snapshot commitment differs")
            generation = artifact["GenerationSet"]["Channels"]["keyword"]
            readiness = generation["Readiness"]
            if readiness["AttemptID"] != artifact["AttemptID"] or readiness["ProfileDigest"] != profile_digest or readiness["GenerationSetID"] != artifact["GenerationSet"]["ID"] or readiness["Generations"][0]["ChunkCount"] != len(snapshot["Records"]):
                raise ValueError("readiness does not match actual records")
            if len(snapshot["Records"]) != 2 or any(r["Published"] for r in snapshot["Records"]):
                raise ValueError("candidate output accidentally marked published")
            for record in snapshot["Records"]:
                if record["GenerationID"] != generation["ID"] or record["Chunk"]["ContentHash"] != hashlib.sha256(record["Chunk"]["Text"].encode("utf-8")).hexdigest() or record["Chunk"]["ProfileDigest"] != profile_digest or record["Citation"]["RevisionID"] != artifact["GenerationSet"]["RevisionID"]:
                    raise ValueError("actual chunk/citation binding differs")
        rows.append({"major": major, "server_version": report["server_version"], "artifacts": 2, "snapshots": 2, "standard_prepare_results": 2, "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Actual keyword preparation and historical positive readback samples; independent full profile/artifact/snapshot hashes and module PrepareResult Schema", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["task completion and final publication CAS", "continuous worker/queue and remote providers", "vector/graph/structured projection pipelines", "production roles, endpoint authorization, migrations and recall/performance"]}


def main():
    path = ROOT / ".local/architecture-check/preparation-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Preparation runtime: 6 actual artifact/snapshot/proof sets passed independent hashes and module Schema across PG15/16/18")


if __name__ == "__main__":
    main()

"""Independent relation checks over actual native PostgreSQL query-read samples."""
import json
from pathlib import Path
import re

from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"query_reads_" + name for name in (
    "real_repeatable_read_pages", "actual_current_approval_epoch", "withdrawal_between_pages",
    "fresh_withdrawal_check", "callback_handle_closed", "cursor_view_scope_binding",
    "detached_return_values", "current_authority_recheck", "authority_failure_scope_denial",
    "cancellation", "store_rotation_fence", "current_approval_revocation")}


def check_sample(sample):
    identity, facts, epoch, current = [sample[k] for k in ("identity", "facts", "epoch", "current")]
    if identity["StoreEpochID"] != "read-store-a" or not re.fullmatch(r"query-view-[0-9a-f]{32}", identity["ReadVersion"]) or not re.fullmatch(r"[0-9a-f]{64}", identity["Watermark"]):
        raise ValueError("invalid provider-neutral read identity")
    ids = [f["DocumentID"] for f in facts]
    if len(facts) < 2 or ids != sorted(set(ids)) or epoch["Number"] != 1 or epoch["KnowledgeBaseID"] != "kb" or epoch["State"] != "active" or not epoch["CutoverWatermark"]:
        raise ValueError("invalid published ordering or active epoch")
    mapping = {}
    for fact in facts:
        generation = fact["GenerationSet"]
        if fact["SpaceID"] != "space" or fact["KnowledgeBaseID"] != "kb" or fact["PublicationEpoch"] < 1 or fact["RowVersion"] < 1 or not fact["ApprovalID"] or not re.fullmatch(r"[0-9a-f]{64}", fact["ContentDigest"]) or generation["RevisionID"] != fact["RevisionID"] or generation["BackendEpochID"] != epoch["ID"] or generation["State"] != "ready":
            raise ValueError("published fact binding differs")
        if set(generation["Channels"]) != {"keyword"}:
            raise ValueError("unexpected native channel")
        channel = generation["Channels"]["keyword"]
        binding, ready = channel["BackendBinding"], channel["Readiness"]
        if channel["RevisionID"] != fact["RevisionID"] or channel["KnowledgeBaseID"] != "kb" or channel["BackendEpochID"] != epoch["ID"] or channel["State"] != "ready" or channel["ProfileDigest"] != epoch["ProfileDigest"] or binding["PlatformResourceID"] != channel["ID"] or binding["GenerationID"] != channel["ID"] or binding["BackendID"] != "prepared/" + channel["ID"] or ready["GenerationSetID"] != generation["ID"] or ready["ContentDigest"] != fact["ContentDigest"]:
            raise ValueError("native readiness metadata differs")
        mapping[fact["RevisionID"]] = generation["ID"]
    if mapping != epoch["RevisionGenerationSets"]:
        raise ValueError("epoch inventory differs")
    first, doc, review = facts[0], current["Document"], current["PublicationReview"]
    if doc["ID"] != first["DocumentID"] or doc["ActiveRevisionID"] != first["RevisionID"] or doc["ActiveGenerationSetID"] != first["GenerationSet"]["ID"] or doc["PublicationEpoch"] != first["PublicationEpoch"] or doc["RowVersion"] != first["RowVersion"] or current["GenerationSet"] != first["GenerationSet"] or current["ActiveBackendEpochID"] != epoch["ID"]:
        raise ValueError("same-view document projection differs")
    if review["Revision"]["ID"] != first["RevisionID"] or review["Revision"]["DocumentID"] != doc["ID"] or review["Revision"]["ContentHash"] != first["ContentDigest"] or review["Approval"]["ID"] != first["ApprovalID"] or review["Validity"]["ApprovalID"] != first["ApprovalID"] or review["Validity"]["ApprovalRowVersion"] != review["Approval"]["RowVersion"] or review["Validity"]["ReviewPolicyVersion"] != 9 or review["Validity"]["State"] != "effective":
        raise ValueError("current approval projection differs")
    withdrawn, revoked, rotated = [sample[k] for k in ("withdrawn_current", "revoked_current", "rotated_identity")]
    wd = withdrawn["Document"]
    if wd["ID"] != doc["ID"] or wd["PublicationEpoch"] != doc["PublicationEpoch"] + 1 or wd["RowVersion"] != doc["RowVersion"] + 1 or wd["ActiveRevisionID"] is not None or wd["ActiveGenerationSetID"] is not None or withdrawn["GenerationSet"] is not None or withdrawn["PublicationReview"] is not None:
        raise ValueError("new observation did not reflect withdrawal")
    if revoked["Document"]["ID"] != facts[1]["DocumentID"] or revoked["PublicationReview"]["Validity"]["State"] == "effective" or revoked["PublicationReview"]["Validity"]["ReviewPolicyVersion"] != 9 or revoked["GenerationSet"] != facts[1]["GenerationSet"]:
        raise ValueError("new observation did not invalidate revoked approval")
    if rotated["StoreEpochID"] != "read-store-b" or rotated["ReadVersion"] == identity["ReadVersion"] or rotated["Watermark"] == identity["Watermark"]:
        raise ValueError("store rotation reused identity")


def validate():
    sources, rows = source_inventory(), []
    for major in (15, 16, 18):
        report = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (report.get("passed") and report.get("requested_major") == major and report.get("sources") == sources and report.get("sources_unchanged_during_checks") and report.get("server_stopped") and report.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete query-read evidence")
        proof = report["go_repository_proof"]
        if not proof.get("passed") or not REQUIRED.issubset(proof.get("checks", [])) or len(proof.get("query_read_samples", [])) != 1:
            raise ValueError("missing actual query-read scenarios")
        check_sample(proof["query_read_samples"][0])
        rows.append({"major": major, "server_version": report["server_version"], "native_read_samples": 1, "go_executable_sha256": report["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Real metadata Repeatable Read, native current approval/epoch, actual withdrawal/revocation and store rotation; independent returned-data relations, not proof of actual index effects or output authorization", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["published Retriever and actual index/evidence output checks", "production IAM/RLS and remote services", "production scale pagination and stateful migration/rollback", "public REST/MCP/UI, sealed Chinese recall and performance"]}


def main():
    path = ROOT / ".local/architecture-check/query-read-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Query reads: actual PostgreSQL observations and independent metadata relations passed on PG15/16/18")


if __name__ == "__main__":
    main()

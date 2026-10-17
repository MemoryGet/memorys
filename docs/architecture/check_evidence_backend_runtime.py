"""Independent relation checks for actual native current-evidence verification."""
import base64
import copy
import hashlib
import json
import re
from datetime import datetime
from pathlib import Path
from jsonschema import Draft202012Validator

from canonical_content import canonical, frozen
from check_architecture import source_inventory
from check_frozen_content import validate_payload

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {"evidence_backend_" + name for name in (
    "real_current_check", "actual_keyword_snapshot", "approved_frozen_body", "actual_original_utf8_quote",
    "index_citation_and_excerpt", "manual_edit_not_original", "independent_original_read_authority",
    "final_control_revocation", "index_deleted_during_projection", "withdrawn_revoked_sources", "logical_chunk_fence")}


def validate_sample(s, proof):
    if s.get("original_grant_budget_clips", 0) < 1 or "original_grant_short_stage_bounded" not in proof["checks"] or s.get("explicit_short_stage_budget_probe") is not True:
        raise ValueError("original grant stage budget was not bounded")
    def instant(value):
        match = re.fullmatch(r'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})', value)
        if not match:
            raise ValueError('invalid actual budget timestamp')
        return datetime.fromisoformat(match[1] + match[3].replace('Z', '+00:00')), int((match[2] or '').ljust(9, '0'))
    budgets = s.get('original_grant_budget_samples', [])
    if not budgets or not any(instant(b['grant']) < instant(b['parent']) for b in budgets):
        raise ValueError('explicit budget clipping evidence missing')
    for budget in budgets:
        if any(instant(budget['grant']) > instant(budget[key]) for key in ('parent', 'scope', 'stage')):
            raise ValueError('original grant exceeds parent/scope/stage')
    if len(proof.get("query_facts_samples", [])) != 1 or s.get("facts_provider") != "postgres-current-references":
        raise ValueError("missing primary current facts")
    current = proof["query_facts_samples"][0]
    if current["scope_kind"] != "verified-profile-snapshot" or current["stored_digest"] != current["facts"]["ProfileDigest"] or current["facts"]["ProfileDigest"] != s["operation"]["Request"]["ProfileDigest"] or current["scope"] != s["operation"]["Scope"] or current["request"] != s["operation"]["Request"]:
        raise ValueError("current binding/request relation differs")
    profile = json.loads(base64.b64decode(current["profile_payload"], validate=True))
    def commitment(domain, body):
        raw = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        return hashlib.sha256(domain.encode() + b"\x00" + canonical(raw)).hexdigest()
    if len(proof.get("profile_assembly_samples", [])) != 1:
        raise ValueError("missing actual profile assembly")
    assembly = proof["profile_assembly_samples"][0]
    if assembly["stage"] != "query" or assembly["profile"] != profile or assembly["request_profile_digest"] != current["stored_digest"] or assembly["candidates"] != proof["keyword_retrieval_samples"][0]["candidates"] or assembly["closed_reference_rejected"] is not True:
        raise ValueError("assembled native result/profile differs")
    route = next(r for r in profile["Query"]["Routes"] if r["ID"] == assembly["route"])
    modules = assembly["identity"]["Modules"]
    matched = [m for m in modules if m["ID"] == route["Binding"]["InstanceID"]]
    if len(matched) != 1 or route["Binding"]["Capability"] not in matched[0]["Capabilities"] or matched[0]["Provider"] != "native-keyword-provider" or matched[0]["ImplementationVersion"] != "1.0.0" or len(assembly["identity"]["ConfigVersion"]) != 64:
        raise ValueError("actual runtime binding differs")
    if not {"profile_assembly_native_retrieval", "profile_assembly_actual_snapshot", "profile_assembly_closed_reference"}.issubset(proof["checks"]):
        raise ValueError("missing assembly scenarios")
    if len(proof.get("knowledge_application_samples", [])) != 1:
        raise ValueError("missing actual shared application")
    app = proof["knowledge_application_samples"][0]
    if app["profile_digest"] != current["stored_digest"] or app["published"]["DocumentID"] != s["operation"]["Expectation"]["Published"][0]["DocumentID"] or app["http_status"] != 200 or app["transport"] != "localhost-http" or app["invocation_authority"] != "host-test-binding" or app["revoked_credential_rejected"] is not True or not app["results"] or len(app["http_results"]) != len(app["results"]):
        raise ValueError("application identity or HTTP evidence differs")
    for candidate, public in zip(app["results"], app["http_results"]):
        citation = candidate["Citation"]
        if public["text"] != candidate["Text"] or public["rank"] != candidate["Rank"] or public["document_id"] != citation["DocumentID"] or public["revision_id"] != citation["RevisionID"] or public["block_id"] != citation["BlockID"] or citation["DocumentID"] != app["published"]["DocumentID"] or candidate["Text"] not in base64.b64decode(s["original_bytes"], validate=True).decode("utf-8") or set(public) != {"text", "rank", "document_id", "revision_id", "block_id", "source_anchor"}:
            raise ValueError("public application result differs from original")
    if not {"knowledge_application_native_search", "knowledge_application_localhost_rest", "knowledge_application_revoked_credential"}.issubset(proof["checks"]):
        raise ValueError("missing native application scenarios")
    index, query, pipeline = [copy.deepcopy(profile[k]) for k in ("Index", "Query", "Pipeline")]
    index_digest = index.pop("Digest")
    if commitment("akos.index-profile/1", {"format": "akos.index-profile/1", "profile": index}) != index_digest:
        raise ValueError("index profile content digest differs")
    query_digest, query["Digest"] = query["Digest"], ""
    if commitment("akos.query-profile/1", query) != query_digest:
        raise ValueError("query profile content digest differs")
    pipeline_digest, pipeline["Digest"] = pipeline["Digest"], ""
    if commitment("akos.pipeline-profile/1", {"Pipeline": pipeline, "IndexDigest": index_digest, "QueryDigest": query_digest}) != pipeline_digest or pipeline_digest != current["stored_digest"] or pipeline["IndexProfile"] != {"ID": index["ID"], "Version": index["Version"]} or pipeline["QueryProfile"] != {"ID": query["ID"], "Version": query["Version"]}:
        raise ValueError("transitive pipeline commitment differs")
    if len(proof.get("knowledge_reranking_samples", [])) != 1:
        raise ValueError("missing native rerank pipeline")
    rerank = proof["knowledge_reranking_samples"][0]
    if rerank["provider"] != "test-reverse-order" or rerank["reranker_calls"] != 3 or rerank["fabrication_rejected"] is not True or rerank["final_revocation_rejected"] is not True or len(rerank["input_candidates"]) < 2 or rerank["requested_limit"] != 1 or len(rerank["ranked_candidates"]) != 1:
        raise ValueError("rerank execution evidence differs")
    for i, candidate in enumerate(rerank["ranked_candidates"]):
        expected_candidate = copy.deepcopy(rerank["input_candidates"][-i-1])
        expected_candidate["Rank"] = i + 1
        if candidate != expected_candidate:
            raise ValueError("rerank changed candidate source lineage")
    for raw_candidate in rerank["input_candidates"]:
        if not any(row["Chunk"]["ID"] == raw_candidate["ChunkID"] and row["Chunk"]["Text"] == raw_candidate["Text"] and row["Citation"] == raw_candidate["Citation"] for row in proof["keyword_retrieval_samples"][0]["actual_rows"]):
            raise ValueError("rerank pool is not actual indexed content")
    rp = rerank["profile"]
    ri, rq, rpipeline = [copy.deepcopy(rp[k]) for k in ("Index", "Query", "Pipeline")]
    rid = ri.pop("Digest")
    rqd, rq["Digest"] = rq["Digest"], ""
    rpd, rpipeline["Digest"] = rpipeline["Digest"], ""
    if commitment("akos.index-profile/1", {"format": "akos.index-profile/1", "profile": ri}) != rid or commitment("akos.query-profile/1", rq) != rqd or commitment("akos.pipeline-profile/1", {"Pipeline": rpipeline, "IndexDigest": rid, "QueryDigest": rqd}) != rpd or rq["Policy"]["EnableRerank"] is not True or rpd == current["stored_digest"]:
        raise ValueError("rerank configuration commitment differs")
    if not {"knowledge_reranking_native_pipeline", "knowledge_reranking_fabrication_rejected", "knowledge_reranking_final_revocation", "knowledge_reranking_candidate_budget_topk"}.issubset(proof["checks"]):
        raise ValueError("missing native reranking scenarios")
    if len(proof.get("knowledge_multiroute_samples", [])) != 1:
        raise ValueError("missing native multi-route application")
    multi = proof["knowledge_multiroute_samples"][0]
    if multi["fuser_calls"] != 1 or multi["pre_fusion_backend_checks"] != 1 or multi["total_backend_checks"] != 2 or multi["provider"] != "same-postgres-reader-two-routes" or multi["route_failure_rejected"] is not True or multi["input_candidates"] != app["results"] or len(multi["candidates"]) != len(app["results"]):
        raise ValueError("multi-route coverage differs")
    mp = multi["profile"]
    mi, mq, mpi = [copy.deepcopy(mp[k]) for k in ("Index", "Query", "Pipeline")]
    mid = mi.pop("Digest")
    mqd, mq["Digest"] = mq["Digest"], ""
    mpd, mpi["Digest"] = mpi["Digest"], ""
    if commitment("akos.index-profile/1", {"format": "akos.index-profile/1", "profile": mi}) != mid or commitment("akos.query-profile/1", mq) != mqd or commitment("akos.pipeline-profile/1", {"Pipeline": mpi, "IndexDigest": mid, "QueryDigest": mqd}) != mpd or len(mq["Routes"]) != 2:
        raise ValueError("multi-route configuration commitment differs")
    routes = {r["ID"]: r for r in mq["Routes"]}
    for i, candidate in enumerate(multi["candidates"]):
        original = next(c for c in app["results"] if c["ChunkID"] == candidate["ChunkID"] and c["Citation"] == candidate["Citation"] and c["Text"] == candidate["Text"])
        origins = candidate["FusionOrigins"]
        if candidate["Rank"] != i + 1 or candidate["ScoreKind"] != "rrf" or len(origins) != 2 or {o["RouteID"] for o in origins} != set(routes) or abs(candidate["Score"] - 2 / (mq["Policy"]["RankConstant"] + original["Rank"])) > 1e-15 or candidate["GenerationIDs"] != original["GenerationIDs"]:
            raise ValueError("independent RRF result differs")
        for origin in origins:
            if origin["RetrieverID"] != routes[origin["RouteID"]]["Binding"]["InstanceID"] or origin["Rank"] != original["Rank"] or origin["GenerationIDs"] != original["GenerationIDs"]:
                raise ValueError("route contribution lineage differs")
    if not {"knowledge_multiroute_native_pipeline", "knowledge_multiroute_fusion_lineage", "knowledge_multiroute_failure_no_partial", "knowledge_multiroute_fuser_preverification", "knowledge_multiroute_fuser_postverification"}.issubset(proof["checks"]):
        raise ValueError("missing multi-route failure scenarios")
    partner = json.loads((ROOT / "docs/architecture/openapi/partner.v1.json").read_text(encoding="utf-8"))
    def partner_validator(name):
        return Draft202012Validator({"$ref": "#/components/schemas/" + name, "components": partner["components"]})
    samples = proof.get("partner_search_samples", [])
    if len(samples) != 3 or {row["case"] for row in samples} != {"omitted", "explicit-empty", "revoked"}:
        raise ValueError("missing partner POST search cases")
    for row in samples:
        partner_validator("SearchRequest").validate(row["request"])
        body = row["response"]
        if row["case"] == "revoked":
            partner_validator("Error").validate(body)
            if row["status"] != 401 or body["code"] != "unauthenticated":
                raise ValueError("revoked partner response differs")
        else:
            partner_validator("SearchResult").validate(body)
            if row["status"] != 200 or body["profile_digest"] != app["profile_digest"] or body["profile_version"] != profile["Pipeline"]["Version"] or body["degraded"] != []:
                raise ValueError("partner trace differs")
            if row["case"] == "explicit-empty":
                if row["request"]["kb_ids"] != [] or body["evidence"] != [] or row["planning_calls"] != 0:
                    raise ValueError("empty KB list expanded")
            else:
                if row.get("transport_path") != partner["servers"][0]["url"] + "/search" or "kb_ids" in row["request"] or len(body["evidence"]) != 1:
                    raise ValueError("omitted KB directory semantics differ")
                evidence = body["evidence"][0]
                if evidence["source_excerpt"] != app["results"][0]["Text"] or evidence["citation"]["document_id"] != app["published"]["DocumentID"]:
                    raise ValueError("partner evidence source differs")
    if not {"partner_search_openapi_post", "partner_search_omitted_kb_current_acl", "partner_search_explicit_empty_no_expansion", "partner_search_revoked_safe_error"}.issubset(proof["checks"]):
        raise ValueError("missing partner transport scenarios")
    mcp_catalog = json.loads((ROOT / "docs/architecture/mcp/tools.v1.json").read_text(encoding="utf-8"))
    tool = next(t for t in mcp_catalog["tools"] if t["name"] == "knowledge_search")
    mcp_samples = proof.get("mcp_gateway_samples", [])
    if len(mcp_samples) != 1:
        raise ValueError("missing native SDK MCP proof")
    mcp = mcp_samples[0]
    if mcp["sdk"] != "github.com/modelcontextprotocol/go-sdk@v1.8.0" or mcp["protocol"] != "2025-11-25" or mcp["mode"] != "controlled-private-bearer" or mcp["revoked_credential_rejected"] is not True or mcp["unsupported_version_rejected"] is not True or mcp["initialize"]["result"]["protocolVersion"] != mcp["protocol"]:
        raise ValueError("MCP transport/version/authentication proof differs")
    listed = mcp["tools_list"]["result"]["tools"]
    if len(listed) != 1 or any(listed[0][key] != tool[key] for key in ("name", "inputSchema", "outputSchema", "annotations")):
        raise ValueError("runtime MCP tool does not match generated contract")
    Draft202012Validator(tool["inputSchema"]).validate(mcp["arguments"])
    result = mcp["search_result"]["result"]
    invalid = mcp["invalid_result"]["result"]
    for item, is_error in ((result, False), (invalid, True)):
        Draft202012Validator(tool["outputSchema"]).validate(item["structuredContent"])
        if item.get("isError", False) is not is_error or len(item["content"]) != 1 or json.loads(item["content"][0]["text"]) != item["structuredContent"]:
            raise ValueError("MCP structured/text/error semantics differ")
    partner_validator("SearchResult").validate(result["structuredContent"])
    if result["structuredContent"]["evidence"] != mcp["rest_result"]["evidence"] or result["structuredContent"]["profile_digest"] != app["profile_digest"] or result["structuredContent"]["profile_version"] != mcp["rest_result"]["profile_version"] or invalid["structuredContent"]["code"] != "invalid_argument":
        raise ValueError("REST/MCP application parity or actor rejection differs")
    if not {"mcp_sdk_native_initialize", "mcp_sdk_search_rest_parity", "mcp_sdk_structured_text_identity", "mcp_sdk_invalid_actor_rejected", "mcp_sdk_version_whitelist", "mcp_sdk_revoked_before_dispatch"}.issubset(proof["checks"]):
        raise ValueError("missing SDK MCP scenarios")
    if current["facts"]["ReviewPolicies"] != {kb: s["observed"]["Documents"][0]["PublicationReview"]["Validity"]["ReviewPolicyVersion"] for kb in current["scope"]["KnowledgeBaseIDs"]}:
        raise ValueError("current review rules differ")
    facts_checks = {"query_facts_" + n for n in ("primary_heads", "not_request_echo", "missing_binding_denied", "acl_version_binding", "profile_immutable", "changed_profile_rejected", "head_rollback_rejected", "profile_payload_verified", "profile_corruption_denied", "catalog_immutable")}
    if not facts_checks.issubset(proof["checks"]):
        raise ValueError("missing current-facts scenarios")
    if len(proof.get("authorization_samples", [])) != 1 or s.get("authorization_provider") != "postgres-sealed-acl":
        raise ValueError("missing native authorization")
    auth = proof["authorization_samples"][0]
    req, scope, grant = auth["request"], auth["scope"], auth["stored_grant"]
    if auth.get("listed_knowledge_bases") != scope["KnowledgeBaseIDs"] or "partner_search_directory_current_identity" not in proof["checks"]:
        raise ValueError("current directory identity/scope proof missing")
    if req["Principal"] != s["operation"]["Principal"] or scope["SubjectID"] != req["Principal"]["SubjectID"] or scope["SpaceID"] != req["SpaceID"] or scope["Action"] != req["Action"] or scope["KnowledgeBaseIDs"] != req["KnowledgeBaseIDs"] or scope["PolicyVersion"] != s["operation"]["Scope"]["PolicyVersion"]:
        raise ValueError("authorized scope/request binding differs")
    if grant != {"space": req["SpaceID"], "version": str(scope["PolicyVersion"]), "issuer": req["Principal"]["Issuer"], "subject": req["Principal"]["SubjectID"], "client": req["Principal"]["ClientID"], "kb": req["KnowledgeBaseIDs"][0], "action": req["Action"]}:
        raise ValueError("actual stored grant differs")
    needed = {"authorization_" + n for n in ("native_sealed_acl", "default_deny", "sealed_grant_immutable", "policy_revocation", "policy_rollback_rejected", "current_check_revocation")}
    if not needed.issubset(proof["checks"]):
        raise ValueError("missing authorization scenarios")
    op, observation, record, blob = [s[k] for k in ("operation", "observed", "actual_index_record", "original_blob")]
    if s["control_refreshes"] != 2 or s["authenticator_calls"] != 2 or s["authorizer_calls"] != 2 or s["identity_provider"] != "rs256_sdk_postgres_status_test_issuer" or len(op["Expectation"]["Published"]) != 1 or len(op["Evidence"]) != 1 or len(observation["Documents"]) != 1:
        raise ValueError("missing full current-check sample")
    fact, evidence, state = op["Expectation"]["Published"][0], op["Evidence"][0], observation["Documents"][0]
    doc, review = state["Document"], state["PublicationReview"]
    if op["Principal"]["SubjectID"] != op["Scope"]["SubjectID"] or op["Scope"]["Action"] != "knowledge.search" or op["Expectation"]["ScopeVersion"] != op["Scope"]["PolicyVersion"] or op["Expectation"]["StoreEpochID"] != observation["Identity"]["StoreEpochID"]:
        raise ValueError("operation/control/source identity differs")
    if doc["ID"] != "evidence-original-document" or fact["DocumentID"] != doc["ID"] or fact["RevisionID"] != doc["ActiveRevisionID"] or fact["GenerationSet"]["ID"] != doc["ActiveGenerationSetID"] or fact["PublicationEpoch"] != doc["PublicationEpoch"] or fact["RowVersion"] != doc["RowVersion"] or fact["GenerationSet"] != state["GenerationSet"] or fact["ApprovalID"] != review["Approval"]["ID"] or review["Validity"]["State"] != "effective" or review["Validity"]["ReviewPolicyVersion"] != 9:
        raise ValueError("actual approved publication binding differs")
    native_records = proof["keyword_retrieval_samples"][0]["actual_rows"]
    if record not in native_records or record["Citation"] != evidence["Citation"] or record["DocumentID"] != doc["ID"] or evidence["SourceExcerpt"] not in record["Chunk"]["Text"] or not evidence["SourceExcerpt"].strip() or record["Published"] is not False:
        raise ValueError("quote is not from actual scoped index data")
    payload = base64.b64decode(s["frozen"]["Payload"], validate=True)
    if canonical(payload) != payload or s["frozen"]["DocumentID"] != doc["ID"] or s["frozen"]["RevisionID"] != fact["RevisionID"]:
        raise ValueError("actual frozen bytes/identity differ")
    value = json.loads(payload)
    validate_payload(value)
    h = frozen(payload)
    if h["content_hash"] != fact["ContentDigest"] or h["content_hash"] != review["Revision"]["ContentHash"] or h["blocks_hash"] != review["Revision"]["BlocksHash"]:
        raise ValueError("independent frozen hashes differ")
    block = next(b for b in value["blocks"] if b["id"] == evidence["Citation"]["BlockID"])
    if block["kind"] != "text" or evidence["SourceExcerpt"] not in block["text"] or block["source_anchor"] != {"kind": "original", "blob_id": blob["ID"], "locator": {"precision": "coarse"}} or evidence["Citation"]["Source"]["BlobID"] != blob["ID"]:
        raise ValueError("quote/approved original anchor differs")
    approved = next(b for b in value["blobs"] if b["id"] == blob["ID"])
    raw = base64.b64decode(s["original_bytes"], validate=True)
    if len(raw) != blob["Size"] or approved["size"] != blob["Size"] or approved["sha256"] != blob["SHA256"] or approved["media_type"] != blob["MediaType"] or hashlib.sha256(raw).hexdigest() != blob["SHA256"] or evidence["SourceExcerpt"] not in raw.decode("utf-8"):
        raise ValueError("quote is not in the actual approved original bytes")


    fence = s["logical_chunk_fence"]
    matching = [r for r in native_records if r["DocumentID"] == fence["document_id"] and r["GenerationID"] == fence["generation_id"]]
    if fence["tombstone_exists"] is not True or fence["retained_rows"] != len(matching) or not matching or not any(r["Chunk"]["ID"] == fence["chunk_id"] for r in matching):
        raise ValueError("logical fence did not retain actual indexed rows")


def validate():
    sources, rows = source_inventory(), []
    for major in (15, 16, 18):
        r = json.loads((ROOT / f".local/architecture-check/postgres{major}-wsl.json").read_text(encoding="utf-8"))
        if not (r.get("passed") and r.get("requested_major") == major and r.get("sources") == sources and r.get("sources_unchanged_during_checks") and r.get("server_stopped") and r.get("temporary_resources_removed")):
            raise ValueError("stale or incomplete backend evidence")
        p = r["go_repository_proof"]
        if not p.get("passed") or "authenticated_control_native_pipeline" not in p.get("checks", []) or not REQUIRED.issubset(p.get("checks", [])) or len(p.get("evidence_backend_samples", [])) != 1:
            raise ValueError("missing real current-evidence scenarios")
        validate_sample(p["evidence_backend_samples"][0], p)
        rows.append({"major": major, "server_version": r["server_version"], "native_current_checks": 1, "actual_original_quotes": 1, "go_executable_sha256": r["go_executable_sha256"]})
    if source_inventory() != sources or len({r["go_executable_sha256"] for r in rows}) != 1:
        raise ValueError("source or probe identity changed")
    return {"passed": True, "production_acceptance": False, "scope": "Actual PostgreSQL CurrentBackend with real keyword index, native approved frozen body and local original bytes; independent quote/source/hash relations; SDK RS256 authentication and actual PostgreSQL status; test issuer keys and seeded data, native sealed ACL and current profile/review references; full profile compilation and deployment assembly remain unverified", "rows": rows, "sources": sources, "sources_unchanged_during_checks": True, "unverified": ["production CurrentControl/IAM/RLS and Grant/module assembly", "model/REST/MCP output and network send fencing", "PDF/Office/OCR/exact geometry, vector/graph and second real backend", "production data volume, Recall@10 and performance", "stateful migration/restart/rollback"]}


def main():
    path = ROOT / ".local/architecture-check/evidence-backend-runtime.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "running"}) + "\n", encoding="utf-8")
    try:
        result = validate()
    except Exception as error:
        path.write_text(json.dumps({"passed": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Current evidence backend: actual native index/body/original quotes passed independent relations on PG15/16/18")


if __name__ == "__main__":
    main()

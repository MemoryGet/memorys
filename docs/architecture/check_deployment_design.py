"""Structural deployment schema fixtures; no module construction or live checks."""
import copy
import json
from pathlib import Path

from jsonschema import Draft202012Validator


def binding(capability):
    return {"instance_id": "design-fixture", "capability": capability,
            "contract": "1.0", "required_features": []}


def fixture():
    roles = ["authenticator", "authorizer", "metadata_uow", "query_read_unit", "publication",
             "blob_store", "task_repository", "task_queue", "event_bus", "audit", "observer"]
    return {
        "schema_version": 1, "build_lock_ref": "design-only-not-a-real-build-lock",
        "modules": [{"instance_id": "design-fixture", "provider": "unimplemented-reference",
                     "implementation_version": "0.1.0", "contract": "1.0", "settings": {}, "dependency_bindings": {}}],
        "bindings": {role: {**binding(role), **({"required_features": ["snapshot.fixed_view", "lifecycle.scoped_read"]} if role == "query_read_unit" else {})} for role in roles},
        "index_profiles": [{"id": "index", "version": 1, "mode": "independent",
                            "bindings": {role: binding(role) for role in ["parser", "splitter", "embedder"]},
                            "index_routes": [{"route_id": "keyword-index", "channels": ["keyword"], "binding": binding("index_writer")},
                                             {"route_id": "vector-index", "channels": ["vector"], "binding": binding("index_writer")}],
                            "policy": {"schema_version": 1, "chunking": {"strategy": "heading", "max_codepoints": 1000, "overlap_codepoints": 100},
                                       "embedding_spec": {"provider_model": "design-fixture", "model_revision": "fixture-1", "dimension": 128, "distance": "cosine", "normalization": "l2", "preprocess_version": "1.0"},
                                       "required_channels": ["keyword", "vector"], "projection_format_version": "1.0"}}],
        "query_profiles": [{"id": "query", "version": 1, "bindings": {"fuser": binding("fuser")},
                            "retrieval_routes": [{"route_id": "keyword-recall", "channels": ["keyword"], "binding": binding("retriever"), "candidate_limit": 50, "required": True},
                                                 {"route_id": "vector-recall", "channels": ["vector"], "binding": binding("retriever"), "candidate_limit": 50, "required": True}],
                            "reflection": {"enabled": False, "max_extra_rounds": 2, "max_plan_queries": 2, "total_budget_ms": 30000},
                            "policy": {"schema_version": 1, "candidate_limit": 100, "answer_evidence_limit": 10, "fusion": {"method": "rrf", "rank_constant": 60},
                                       "rerank": {"enabled": False}, "generation": {"enabled": False, "max_output_tokens": 2048},
                                       "timeouts": {"retrieval_ms": 5000, "rerank_ms": 3000, "generation_ms": 20000}, "optional_capabilities": []}}],
        "pipeline_profiles": [{"id": "default", "version": 1,
                               "index_profile_ref": {"id": "index", "version": 1},
                               "query_profile_ref": {"id": "query", "version": 1}}]
    }


def run():
    root = Path(__file__).resolve().parent
    schema = json.loads((root / "schemas/deployment-config.v1.schema.json").read_text())
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema)
    good = fixture()
    validator.validate(good)
    edits = [
        lambda v: v.update(unknown=True),
        lambda v: v["bindings"].pop("authorizer"),
        lambda v: v["modules"][0].update(implementation_version="latest"),
        lambda v: v["modules"][0].update(secret="not-a-real-secret"),
        lambda v: v["query_profiles"][0]["bindings"].update(authorizer=binding("authorizer")),
        lambda v: v["index_profiles"][0]["bindings"].update(publication=binding("publication")),
        lambda v: v["query_profiles"][0]["reflection"].update(max_extra_rounds=3),
        lambda v: v["query_profiles"][0]["reflection"].update(total_budget_ms=30001),
        lambda v: v["pipeline_profiles"][0]["index_profile_ref"].pop("version"),
        lambda v: v["modules"][0]["dependency_bindings"].update(reader="untyped-instance"),
        lambda v: v["index_profiles"][0]["policy"].update(disable_acl=True),
        lambda v: v["query_profiles"][0]["policy"].update(include_drafts=True),
        lambda v: v["index_profiles"][0]["policy"]["embedding_spec"].update(dimension=0),
        lambda v: v["index_profiles"][0]["policy"]["chunking"].update(strategy="parent_child"),
        lambda v: v["query_profiles"][0]["policy"]["fusion"].update(method="raw_score_sum"),
        lambda v: v["query_profiles"][0]["policy"].update(optional_capabilities=["authorizer"]),
        lambda v: v["query_profiles"][0]["policy"]["generation"].update(api_key="not-a-real-secret"),
        lambda v: v["query_profiles"][0].update(retrieval_routes=[]),
        lambda v: v["index_profiles"][0].update(index_routes=[]),
        lambda v: v["query_profiles"][0]["retrieval_routes"][0].update(channels=[]),
        lambda v: v["query_profiles"][0]["retrieval_routes"][0].update(candidate_limit=0),
        lambda v: v["query_profiles"][0]["retrieval_routes"][0].pop("required"),
        lambda v: v["query_profiles"][0]["retrieval_routes"][0].update(binding="untyped-provider"),
    ]
    for i, edit in enumerate(edits):
        bad = copy.deepcopy(good)
        edit(bad)
        if validator.is_valid(bad):
            raise AssertionError(f"Invalid fixture {i} accepted")
    print(f"PASS: deployment schema, structural fixture, {len(edits)} rejected configurations")
    print("Fixture provider is unimplemented. Graph, policies, epochs and live modules not certified.")


if __name__ == "__main__":
    run()

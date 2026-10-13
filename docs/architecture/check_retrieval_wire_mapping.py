"""Actual Go DTO round trips, not a retrieval/model/permission benchmark."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build
from canonical_content import canonical
from check_parser_wire_mapping import invoke, PROJECT
from remote_wire_validation import validate


def fixtures():
    context = {"request_id": "r", "invocation_id": "i", "module_instance_id": "retriever-a", "contract_version": "1.0",
               "deadline": "2026-10-12T12:00:00Z", "profile_digest": "a" * 64,
               "scope": {"scope_id": "scope", "subject_id": "subject", "action": "knowledge.search", "space_id": "space-1",
                         "kb_ids": ["kb-1"], "policy_version": 9007199254740993, "expires_at": "2026-10-12T12:00:00Z"}}
    spec = {"provider_model": "fixture", "model_revision": "1", "dimension": 2, "distance": "cosine", "normalization": "l2", "preprocess_version": "1"}
    generation = {"id": "gen-1", "kb_id": "kb-1", "revision_id": "rev-1", "channel": "vector", "profile_digest": "a" * 64,
                  "embedding_spec": spec, "backend_epoch_id": "epoch-1", "state": "ready",
                  "backend_binding": {"platform_resource_type": "generation", "platform_resource_id": "gen-1", "module_instance_id": "index-a",
                                      "backend_id": "opaque-fixture", "generation_id": "gen-1", "purpose": "retrieval"}}
    generation["readiness"] = {"generation_set_id": "set-1", "attempt_id": "attempt-1", "content_digest": "c" * 64, "profile_digest": "a" * 64,
                               "generations": [{"generation_id": "gen-1", "channel": "vector", "backend_epoch_id": "epoch-1", "chunk_count": 1, "embedding_spec_digest": "d" * 64}]}
    candidate = {"citation": {"document_id": "doc-1", "revision_id": "rev-1", "block_id": "block-1",
                  "source_anchor": {"kind": "revision", "revision_block_id": "block-1", "locator": {"precision": "coarse"}}},
                 "chunk_id": "chunk-1", "route_id": "route-1", "retriever_id": "retriever-a", "kb_id": "kb-1", "rank": 1,
                 "score": 0.1, "score_kind": "similarity", "generation_ids": ["gen-1"], "text": "中文证据😀"}
    inputs = {
        "embedder_embed": {"texts": ["中文", "证据"], "spec": spec},
        "retriever_search": {"query": {"text": "故障", "vector": [0.1, 0.9], "embedding_spec": spec},
            "publication": {"space_id": "space-1", "kb_ids": ["kb-1"], "revision_ids": ["rev-1"], "generation_ids": ["gen-1"], "snapshot_digest": "b" * 64},
            "generations": [generation], "backend_epoch_id": "epoch-1", "route_id": "route-1", "channels": ["vector"], "limit": 10},
        "reranker_rerank": {"query": "故障", "candidates": [candidate], "limit": 1}}
    results = {"embedder_embed": {"spec": spec, "vectors": [[0.1, 0.9], [0.2, 0.8]]}, "retriever_search": {"candidates": [candidate]},
               "reranker_rerank": {"evidence": [{"citation": candidate["citation"], "source_excerpt": "中文证据😀", "rank": 1}]}}
    return [{"operation": name, "request": {"context": copy.deepcopy(context), "input": copy.deepcopy(value)},
             "response": {"request_id": "r", "invocation_id": "i", "result": copy.deepcopy(results[name])}} for name, value in inputs.items()]


def run():
    schema = build()
    with tempfile.TemporaryDirectory() as directory:
        executable = Path(directory) / "wire.exe"
        subprocess.run([str(PROJECT / ".tools/go/bin/go.exe"), "build", "-o", str(executable), "./cmd/architecture-parser-wire"], cwd=PROJECT, check=True, capture_output=True, timeout=60)
        for case in fixtures():
            name = case["operation"]
            validate(schema, name + "_Request", case["request"]); validate(schema, name + "_Response", case["response"])
            result = invoke(executable, case)
            if result.returncode: raise AssertionError("Go mapping failed: " + name + ": " + result.stderr)
            output = json.loads(result.stdout)
            validate(schema, name + "_Request", output["request"]); validate(schema, name + "_Response", output["response"])
            expected = {"request": case["request"], "response": case["response"]}
            if canonical(result.stdout.encode()) != canonical(json.dumps(expected, ensure_ascii=False).encode()):
                raise AssertionError("Fields changed: " + name)
        bad = copy.deepcopy(fixtures()[1]); bad["response"]["result"]["candidates"][0]["generation_ids"] = ["outside-generation"]
        if invoke(executable, bad).returncode == 0: raise AssertionError("Out-of-filter evidence accepted")
    print("PASS: embedder/retriever/reranker actual Go/wire round trips, before/after schema and out-of-filter evidence rejection")
    print("No model execution, backend filtering, access grants, publication or network tests")


if __name__ == "__main__":
    run()

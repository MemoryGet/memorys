"""Typed index/projection/control DTO checks; no backend writes, verification or deletion."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build
from canonical_content import canonical
from check_parser_wire_mapping import invoke, PROJECT
from check_retrieval_wire_mapping import fixtures as retrieval_fixtures
from remote_wire_validation import validate


def fixtures():
    source = retrieval_fixtures()[1]
    context = copy.deepcopy(source["request"]["context"])
    context["module_instance_id"] = "index-a"; context["scope"]["action"] = "index.prepare"
    generation = copy.deepcopy(source["request"]["input"]["generations"][0])
    generation.update(state="building"); generation.pop("readiness"); generation.pop("backend_binding")
    chunk = {"id": "chunk-1", "revision_id": "rev-1", "spans": [{"block_id": "block-1", "char_start": 0, "char_end": 3}],
             "text": "中文😀", "profile_digest": "a" * 64, "content_hash": "b" * 64}
    target = {"generation_set_id": "set-1", "generations": [generation], "operation_id": "operation-1", "attempt_id": "attempt-1",
              "fencing_token": 9007199254740993, "chunks": [chunk]}
    proof = {"generation_set_id": "set-1", "attempt_id": "attempt-1", "content_digest": "c" * 64, "profile_digest": "a" * 64,
             "generations": [{"generation_id": "gen-1", "channel": "vector", "backend_epoch_id": "epoch-1", "chunk_count": 1, "embedding_spec_digest": "d" * 64}]}
    expectation = {"content_digest": "c" * 64, "chunk_counts": {"gen-1": 1}, "embedding_spec_digests": {"gen-1": "d" * 64}}
    result = {"proof": proof, "bindings": [{"generation_id": "gen-1", "opaque_backend_ref": "private-fixture-ref"}]}
    operations = {"index_writer_prepare": (dict(target, vectors=[[0.1, 0.9]]), result),
                  "composite_ingest_search_prepare_projection": (dict(target, projection_format_version="1.0"), result)}
    for capability in ["index_writer", "composite_ingest_search"]:
        operations[capability + "_verify"] = ({"proof": proof}, {"completed": True})
        operations[capability + "_delete"] = ({"generation_id": "gen-1", "chunk_ids": ["chunk-1"], "mode": "chunks"}, {"completed": True})
    return [{"operation": name, "request": {"context": copy.deepcopy(context), "input": copy.deepcopy(value)},
             "response": {"request_id": "r", "invocation_id": "i", "result": copy.deepcopy(output)},
             "expectation": expectation} for name, (value, output) in operations.items()]


def run():
    schema = build()
    with tempfile.TemporaryDirectory() as directory:
        executable = Path(directory) / "index-inspect.exe"
        subprocess.run([str(PROJECT / ".tools/go/bin/go.exe"), "build", "-o", str(executable), "./cmd/architecture-parser-wire"], cwd=PROJECT, check=True, capture_output=True, timeout=60)
        for case in fixtures():
            name = case["operation"]
            validate(schema, name + "_Request", case["request"]); validate(schema, name + "_Response", case["response"])
            result = invoke(executable, case)
            if result.returncode: raise AssertionError("Go mapping failed: " + name + ": " + result.stderr)
            output = json.loads(result.stdout)
            validate(schema, name + "_Request", output["request"]); validate(schema, name + "_Response", output["response"])
            if canonical(result.stdout.encode()) != canonical(json.dumps({"request": case["request"], "response": case["response"]}, ensure_ascii=False).encode()):
                raise AssertionError("Index fields changed: " + name)
        for mutation in [lambda c: c["response"]["result"]["proof"].update(attempt_id="old-attempt"),
                         lambda c: c["response"]["result"]["bindings"][0].update(generation_id="outside-generation"),
                         lambda c: c["response"]["result"]["proof"]["generations"][0].update(chunk_count=2)]:
            bad = copy.deepcopy(fixtures()[0]); mutation(bad)
            if invoke(executable, bad).returncode == 0: raise AssertionError("Mismatched prepare proof accepted")
    print("PASS: 6 actual Go/wire index/composite prepare/verify/delete operations, independent before/after schemas and 3 rejected proof/binding mismatches")
    print("No real index build, Verify, lease ownership, authorization, deletion, publication or migration proof")


if __name__ == "__main__":
    run()

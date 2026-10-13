"""Validate parser fixtures against schemas and the actual Go typed codec."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build
from canonical_content import canonical
from remote_wire_validation import validate

ROOT = Path(__file__).resolve().parent
PROJECT = ROOT.parent.parent


def fixture_cases():
    suite = json.loads((ROOT / "examples/frozen-content-golden.v1.json").read_text(encoding="utf-8"))
    cases = []
    for sample in suite["cases"]:
        content = json.loads(sample["input_json"])
        invocation = "invoke-" + sample["name"]
        context = {"request_id": "req-1", "invocation_id": invocation, "module_instance_id": "parser-a", "contract_version": "1.0",
                   "deadline": "2026-10-12T12:00:00.123456789Z", "profile_digest": "a" * 64,
                   "scope": {"scope_id": "scope-1", "subject_id": "subject-1", "space_id": "space-1", "action": "document.parse",
                             "kb_ids": ["kb-1"], "policy_version": 9007199254740993, "expires_at": "2026-10-12T12:00:00.123456789Z"}}
        request = {"context": context, "input": {"draft_id": "draft-1", "input": content["blobs"][0], "format": "fixture",
                   "options": {"languages": ["zh"], "enable_ocr": True}}}
        response = {"request_id": "req-1", "invocation_id": invocation,
                    "result": {"blocks": content["blocks"], "diagnostics": [], "parser_version": "fixture-1"}}
        cases.append({"name": sample["name"], "request": request, "response": response})
    return cases


def invoke(executable, value):
    return subprocess.run([str(executable)], input=json.dumps(value, ensure_ascii=False),
                          capture_output=True, text=True, encoding="utf-8", timeout=10)


def run():
    spec = build()
    go = PROJECT / ".tools/go/bin/go.exe"
    with tempfile.TemporaryDirectory() as directory:
        executable = Path(directory) / "parser-wire-inspect.exe"
        subprocess.run([str(go), "build", "-o", str(executable), "./cmd/architecture-parser-wire"], cwd=PROJECT, check=True, capture_output=True, timeout=60)
        for case in fixture_cases():
            value = {"request": case["request"], "response": case["response"]}
            validate(spec, "parser_parse_Request", value["request"])
            validate(spec, "parser_parse_Response", value["response"])
            result = invoke(executable, value)
            if result.returncode:
                raise AssertionError("Go mapping failed: " + case["name"] + ": " + result.stderr)
            output = json.loads(result.stdout)
            validate(spec, "parser_parse_Request", output["request"])
            validate(spec, "parser_parse_Response", output["response"])
            if canonical(result.stdout.encode("utf-8")) != canonical(json.dumps(value, ensure_ascii=False).encode("utf-8")):
                raise AssertionError("Wire fields changed: " + case["name"])
        bad = copy.deepcopy(value)
        bad["response"]["invocation_id"] = "another-invocation"
        if invoke(executable, bad).returncode == 0:
            raise AssertionError("Mismatched response was accepted")
    print("PASS: 7 typed parser request/result round trips, before/after schemas, exact large integers/nanoseconds and mismatched invocation rejection")
    print("No parser execution, access grants, source/media validation or network behavior tested")


if __name__ == "__main__":
    run()

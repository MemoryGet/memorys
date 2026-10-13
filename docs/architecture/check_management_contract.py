"""Validate management design references and DTOs, not live routes or authorization."""
import json
import re

from jsonschema import Draft202012Validator

from build_management_contract import OUTPUT, generate, serialized


def run():
    assert OUTPUT.read_text(encoding="utf-8") == serialized(), "Generated management design stale"
    spec = json.loads(OUTPUT.read_text(encoding="utf-8"))
    assert spec == generate()
    schemas = spec["components"]["schemas"]
    for schema in schemas.values():
        Draft202012Validator.check_schema(schema)

    def walk(value):
        if isinstance(value, dict):
            if "$ref" in value:
                assert value["$ref"].startswith("#/"), "Unexpected external reference"
                node = spec
                for key in value["$ref"][2:].split("/"):
                    node = node[key.replace("~1", "/").replace("~0", "~")]
            for child in value.values():
                walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)
    walk(spec)
    ids = set()
    for path, operations in spec["paths"].items():
        for method, op in operations.items():
            assert op["operationId"] not in ids
            ids.add(op["operationId"])
            declared = {p["name"] for p in op["parameters"] if p["in"] == "path" and p["required"]}
            assert declared == set(re.findall(r"\{([^}]+)\}", path)), path
            assert op["x-required-permission"] and op["responses"]
            if method != "get":
                assert any(p["name"] == "Idempotency-Key" and p["required"] for p in op["parameters"])
    assert len(ids) == 26

    def validator(name):
        return Draft202012Validator({"$ref": "#/components/schemas/" + name, "components": spec["components"]})

    samples = {
        "CreateKB": {"name": "资料库"}, "SetGrant": {"role": "Viewer", "expected_policy_version": 1},
        "CreateDraft": {"base_revision_id": None, "expected_document_version": 1},
        "EditDraft": {"expected_version": 1, "blocks": []},
        "ParseDraft": {"expected_version": 1, "pipeline_profile_id": "default", "pipeline_profile_version": 1},
        "SubmitDraft": {"expected_version": 1}, "DecideApproval": {"decision": "approve", "reason": "核验通过", "expected_approval_version": 0},
        "PublishDocument": {"revision_id": "rev-1", "approval_id": "approval-1", "expected_publication_epoch": 0},
        "WithdrawDocument": {"reason": "内容失效", "expected_publication_epoch": 1},
        "JobCommand": {"expected_job_version": 1}, "ProposeDraft": {"comment": "核对参数"},
    }
    for name, sample in samples.items():
        validator(name).validate(sample)
    failures = [
        ("PublishDocument", {"revision_id": "rev-1", "approval_id": "approval-1"}),
        ("PublishDocument", dict(samples["PublishDocument"], actor_id="spoofed")),
        ("SetGrant", {"role": "Owner", "expected_policy_version": 1}),
        ("EditDraft", {"expected_version": 0, "blocks": []}),
        ("ParseDraft", dict(samples["ParseDraft"], pipeline_profile_version="latest")),
        ("DecideApproval", dict(samples["DecideApproval"], decision="auto_publish")),
        ("WithdrawDocument", dict(samples["WithdrawDocument"], expected_publication_epoch=-1)),
    ]
    for name, sample in failures:
        assert not validator(name).is_valid(sample), name
    print(f"PASS: 26 operations, local refs, {len(schemas)} schemas, required paths/idempotency, 11 requests and 7 rejected DTOs")
    print("No full OpenAPI conformance, response semantics, live permissions, uploads, CAS or UI was tested.")


if __name__ == "__main__":
    run()

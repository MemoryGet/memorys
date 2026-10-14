"""Validate architecture schema and fixtures; not a runtime registry test.

Run: python docs/architecture/check_manifest_design.py
Requires the jsonschema Python package in the local documentation environment.
"""
import copy
import json
from pathlib import Path

from jsonschema import Draft202012Validator


def validate_manifest(value, validator):
    validator.validate(value)
    Draft202012Validator.check_schema(value["config_schema"])
    names = [item["name"] for item in value["capabilities"]]
    slots = [item["slot"] for item in value["dependencies"]]
    if len(set(names)) != len(names) or len(set(slots)) != len(slots):
        raise ValueError("duplicate capability name or dependency slot")
    policy = json.loads((Path(__file__).resolve().parents[2] / "pkg/kernel/extension-features.v1.json").read_text(encoding="utf-8"))
    if policy["schema_version"] != 1:
        raise ValueError("unsupported extension policy")
    for item in value["capabilities"]:
        if not set(policy["capabilities"].get(item["name"], [])) <= set(item["features"]):
            raise ValueError("minimum extension capability features missing")
    grouped = set()
    group_ids = set()
    for group in value["composite_groups"]:
        members = set(group["capabilities"])
        if group["id"] in group_ids or not members <= set(names) or members & grouped:
            raise ValueError("invalid composite group")
        grouped.update(members)
        group_ids.add(group["id"])


def run():
    root = Path(__file__).resolve().parent
    schema = json.loads((root / "schemas/module-manifest.v1.schema.json").read_text())
    sample = json.loads((root / "examples/parser-manifest.json").read_text())
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema)
    validate_manifest(sample, validator)
    stateful = copy.deepcopy(sample)
    stateful.update(state_model="authoritative", migration_modes=["export_import"])
    validate_manifest(stateful, validator)
    cases = []

    def invalid(name, edit):
        value = copy.deepcopy(sample)
        edit(value)
        cases.append((name, value))

    invalid("unknown field", lambda v: v.update(secret="not-a-real-secret"))
    invalid("unlocked version", lambda v: v.update(implementation_version="latest"))
    invalid("empty capabilities", lambda v: v.update(capabilities=[]))
    invalid("missing health", lambda v: v.pop("health"))
    invalid("no migration for state", lambda v: v.update(state_model="derived"))
    invalid("authority cannot only rebuild", lambda v: v.update(state_model="authoritative", migration_modes=["rebuild"]))
    invalid("stateless migration", lambda v: v.update(migration_modes=["copy_verify"]))
    invalid("invalid config schema", lambda v: v.update(config_schema={"type": "not-a-type"}))
    invalid("duplicate capability name", lambda v: v["capabilities"].append({"name": "parser", "version": "1.1", "features": []}))
    invalid("duplicate dependency slot", lambda v: v["dependencies"].append(dict(v["dependencies"][0], optional=True)))
    invalid("undeclared composite member", lambda v: v.update(composite_groups=[{"id": "group", "capabilities": ["retriever"], "replacement_unit": "whole_group", "reason": "fixture"}]))
    invalid("negative timeout", lambda v: v["health"].update(startup_timeout_ms=-1))
    from jsonschema.exceptions import SchemaError, ValidationError
    for name, value in cases:
        try:
            validate_manifest(value, validator)
        except (ValueError, SchemaError, ValidationError):
            continue
        raise AssertionError(f"Unexpected acceptance: {name}")
    settings = Draft202012Validator(sample["config_schema"])
    settings.validate({"max_input_bytes": 4096})
    if settings.is_valid({"max_input_bytes": 0}) or settings.is_valid({"max_input_bytes": 4096, "extra": True}):
        raise AssertionError("Parser settings constraints were not enforced")
    print(f"PASS: schema, 2 manifest fixtures, {len(cases)} rejected manifests, settings boundaries")
    print("Not covered: deployment graph, live health, migration or retrieval behavior.")


if __name__ == "__main__":
    run()

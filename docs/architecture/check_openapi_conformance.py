"""Validate OpenAPI design specifications with the pinned external validator."""
import argparse
import copy
from datetime import datetime, timezone
import hashlib
from importlib.metadata import version
import json
from pathlib import Path

from jsonschema.exceptions import ValidationError
from openapi_spec_validator import validate

ROOT = Path(__file__).resolve().parent


def run(write_report=False):
    tool_version = version("openapi-spec-validator")
    if tool_version != "0.9.0":
        raise SystemExit("Use the reviewed validator release: openapi-spec-validator==0.9.0")
    records = []
    for filename in ["partner.v1.json", "management.v1.json", "modules.v1.json"]:
        path = ROOT / "openapi" / filename
        raw = path.read_bytes()
        spec = json.loads(raw)
        validate(spec)
        invalid = copy.deepcopy(spec)
        invalid.pop("info")
        try:
            validate(invalid)
        except ValidationError:
            pass
        else:
            raise AssertionError("Validator failed to reject missing required OpenAPI info")
        operations = sum(method in {"get", "put", "post", "delete", "patch", "head", "options", "trace"}
                         for item in spec["paths"].values() for method in item)
        records.append({"path": "openapi/" + filename, "sha256": hashlib.sha256(raw).hexdigest(),
                        "openapi_version": spec["openapi"], "operations": operations,
                        "result": "pass", "negative_control": "missing_info_rejected"})
        print(f"PASS: {filename}, OpenAPI {spec['openapi']}, {operations} operations")
    report = {"checked_at": datetime.now(timezone.utc).isoformat(),
              "validator": {"name": "openapi-spec-validator", "version": tool_version},
              "scope": "OpenAPI structural conformance; no handler, SDK, authorization, migration or quality tests",
              "specifications": records}
    if write_report:
        output = ROOT / "evidence/openapi-conformance.json"
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print("Saved specification hashes and validation evidence")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write-report", action="store_true")
    run(parser.parse_args().write_report)

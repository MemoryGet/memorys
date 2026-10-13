"""Inspect supplied deployment files offline; never construct providers or claim readiness."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import sys

from jsonschema import Draft202012Validator
from jsonschema.exceptions import SchemaError, ValidationError
from referencing.exceptions import Unresolvable

from check_assembly_semantics import local_schema, validate
from check_manifest_design import validate_manifest

ROOT = Path(__file__).resolve().parent
LIMIT = 4 << 20


def object_pairs(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON key")
        value[key] = item
    return value


def reject_constant(_value):
    raise ValueError("non-finite JSON number")


def finite_float(value):
    result = float(value)
    if not math.isfinite(result):
        raise ValueError("non-finite JSON number")
    return result


def check_depth(text):
    depth, quoted, escaped = 0, False, False
    for char in text:
        if quoted:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quoted = False
        elif char == '"':
            quoted = True
        elif char in "[{":
            depth += 1
            if depth > 64:
                raise ValueError("JSON nesting exceeds limit")
        elif char in "]}":
            depth -= 1


def read_object(path):
    with path.open("rb") as stream:
        data = stream.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise ValueError("JSON size exceeds limit")
    text = data.decode("utf-8")
    check_depth(text)
    value = json.loads(text, object_pairs_hook=object_pairs, parse_constant=reject_constant,
                       parse_float=finite_float)
    if not isinstance(value, dict):
        raise ValueError("JSON root must be an object")
    return value, hashlib.sha256(data).hexdigest()


def inspect(config_path, manifest_paths):
    config, config_hash = read_object(config_path)
    providers, hashes = {}, []
    schema = json.loads((ROOT / "schemas/module-manifest.v1.schema.json").read_text(encoding="utf-8"))
    for path in manifest_paths:
        manifest, digest = read_object(path)
        validate_manifest(manifest, Draft202012Validator(schema))
        local_schema(manifest["config_schema"])
        key = (manifest["module_id"], manifest["implementation_version"])
        if key in providers:
            raise ValueError("duplicate provider release")
        providers[key] = manifest
        hashes.append({"provider": key[0], "implementation_version": key[1], "sha256": digest})
    order = validate(config, providers)
    return {"report_version": 1, "status": "offline_validated", "ready_for_service": False,
            "config_sha256": config_hash, "manifests": sorted(hashes, key=lambda h: (h["provider"], h["implementation_version"])),
            "creation_order": order,
            "unverified": ["artifact_lock", "runtime_factories", "health", "credentials", "backend_layout",
                           "persistent_epochs", "kb_fanout", "authorization", "migration", "retrieval_quality"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, action="append", required=True)
    args = parser.parse_args()
    try:
        report = inspect(args.config, args.manifest)
    except (ValidationError, SchemaError):
        print("validation_failed: schema or configuration constraints failed", file=sys.stderr)
        return 1
    except (OSError, UnicodeError):
        print("validation_failed: input file could not be read as UTF-8", file=sys.stderr)
        return 1
    except (Unresolvable, RecursionError):
        print("validation_failed: schema reference could not be resolved locally", file=sys.stderr)
        return 1
    except ValueError as error:
        # These messages originate in validators; never include source values.
        message = "invalid JSON syntax" if isinstance(error, json.JSONDecodeError) else str(error)
        print("validation_failed: " + message, file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

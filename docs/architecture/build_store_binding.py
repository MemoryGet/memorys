"""Offline review envelope; not a signed control-plane expectation."""
import argparse
import json
from pathlib import Path


def build():
    text = {"type": "string", "minLength": 1, "maxLength": 4096}
    version = {"type": "integer", "minimum": 1, "maximum": 18446744073709551615}
    def obj(props): return {"type": "object", "additionalProperties": False, "required": list(props), "properties": props}
    identity = obj({"dataset_id": text, "metadata_store_epoch_id": text, "journal_stream_id": text,
                    "binding_version": version, "metadata_model_version": version,
                    "schema_digest": {"type": "string", "pattern": "^[a-f0-9]{64}$"}})
    endpoint = obj({"role": {"enum": ["metadata_uow", "query_read_unit", "metadata_repository", "task_repository", "audit"]},
                    "module_instance_id": text, "endpoint_identity": text})
    expected = obj({"binding_id": text, "identity": identity, "activation_cursor": text,
                    "endpoints": {"type": "array", "minItems": 2, "maxItems": 5, "items": endpoint}})
    observation = obj({"observation_id": text, "binding_id": text, "identity": identity, "endpoint": endpoint, "current_cursor": text})
    return {"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "akos.store-binding-review/1",
            **obj({"schema_version": {"type": "integer", "const": 1}, "expectation": expected,
                   "observations": {"type": "array", "minItems": 2, "maxItems": 5, "items": observation}})}


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__); p.add_argument("--write", action="store_true"); a = p.parse_args()
    path = Path(__file__).parent / "schemas/store-binding-review.v1.schema.json"
    data = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if a.write: path.write_text(data, encoding="utf-8")
    elif path.read_text(encoding="utf-8") != data: raise SystemExit("store binding review schema is stale")
    else: print("PASS: store binding review schema generation consistency")

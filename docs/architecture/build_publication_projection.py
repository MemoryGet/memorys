"""Closed publication projection draft, not an authorization service."""
import argparse
import json
from pathlib import Path


def build():
    ident = {"type": "string", "minLength": 1, "maxLength": 200}
    digest = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
    version = {"type": "integer", "minimum": 1, "maximum": 18446744073709551615}
    channel = {"enum": ["keyword", "vector", "graph"]}
    def obj(properties):
        return {"type": "object", "additionalProperties": False, "required": list(properties), "properties": properties}
    entry = obj({"document_id": ident, "revision_id": ident, "publication_version": version,
                 "generation_set_id": ident, "generation_id": ident, "channel": channel, "index_profile_digest": digest})
    props = {"schema_version": {"type": "integer", "const": 1}}
    for key in ("projection_id", "request_id", "invocation_id", "parent_scope_id", "child_scope_id", "space_id", "knowledge_base_id", "backend_epoch_id", "route_id", "module_instance_id"):
        props[key] = ident
    for key in ("parent_publication_digest", "query_profile_digest", "pipeline_digest"):
        props[key] = digest
    props.update({"policy_version": version, "channels": {"type": "array", "minItems": 1, "maxItems": 3, "uniqueItems": True, "items": channel},
                  "entries": {"type": "array", "minItems": 1, "maxItems": 1000, "items": entry},
                  "expires_at_unix_ns": {"type": "integer", "minimum": 1, "maximum": 9223372036854775807}})
    return {"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "akos.publication-projection/1", **obj(props)}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    path = Path(__file__).parent / "schemas/publication-projection.v1.schema.json"
    text = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.write:
        path.write_text(text, encoding="utf-8")
    elif path.read_text(encoding="utf-8") != text:
        raise SystemExit("publication projection schema is stale")
    else:
        print("PASS: publication projection schema generation consistency")

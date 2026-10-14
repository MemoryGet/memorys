"""Generate optional memory record wire draft; no provider or authorization."""
import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def build():
    source = json.loads((ROOT / "openapi/partner.v1.json").read_text(encoding="utf-8"))["components"]["schemas"]
    defs = {}

    def convert(value):
        if isinstance(value, dict):
            if "$ref" in value:
                prefix = "#/components/schemas/"
                ref = value["$ref"]
                if not ref.startswith(prefix):
                    raise ValueError("Unexpected external schema reference")
                name = ref[len(prefix):]
                if name not in defs:
                    defs[name] = {}  # Protect recursive traversal.
                    defs[name] = convert(copy.deepcopy(source[name]))
                return {**value, "$ref": "#/$defs/" + name}
            return {k: convert(v) for k, v in value.items()}
        if isinstance(value, list):
            return [convert(v) for v in value]
        return value

    citation = convert({"$ref": "#/components/schemas/Citation"})
    identifier = {"$ref": "#/$defs/ID"}
    provenance = {"type": "object", "additionalProperties": False,
                  "required": ["kind", "citations"],
                  "properties": {"kind": {"enum": ["user_confirmed", "task_experience", "knowledge_summary"]},
                                 "confirmation_id": identifier, "task_id": identifier, "feedback_id": identifier,
                                 "citations": {"type": "array", "maxItems": 256, "items": citation}},
                  "oneOf": [
                      {"properties": {"kind": {"const": "user_confirmed"}}, "required": ["confirmation_id"],
                       "not": {"anyOf": [{"required": ["task_id"]}, {"required": ["feedback_id"]}]}},
                      {"properties": {"kind": {"const": "task_experience"}},
                       "anyOf": [{"required": ["task_id"]}, {"required": ["feedback_id"]}],
                       "not": {"required": ["confirmation_id"]}},
                      {"properties": {"kind": {"const": "knowledge_summary"}, "citations": {"minItems": 1}},
                       "not": {"anyOf": [{"required": ["confirmation_id"]}, {"required": ["task_id"]}, {"required": ["feedback_id"]}]}}
                  ]}
    timestamp = {"type": "string", "format": "date-time"}
    return {"$schema": "https://json-schema.org/draft/2020-12/schema",
            "title": "AI Knowledge OS optional memory record V1 draft",
            "type": "object", "additionalProperties": False,
            "required": ["schema_version", "id", "owner_id", "space_id", "content", "provenance", "created_at", "version"],
            "properties": {"schema_version": {"const": "1.0"}, "id": identifier,
                           "owner_id": identifier, "space_id": identifier, "kb_id": identifier, "task_id": identifier,
                           "content": {"type": "string", "minLength": 1, "maxLength": 16000},
                           "provenance": provenance, "created_at": timestamp, "expires_at": timestamp,
                           "version": {"type": "integer", "minimum": 1, "maximum": 18446744073709551615}},
            "$defs": defs}


if __name__ == "__main__":
    (ROOT / "schemas/memory-record.v1.schema.json").write_text(
        json.dumps(build(), ensure_ascii=False, indent=2)+"\n", encoding="utf-8")

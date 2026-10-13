"""Bundle the frozen content envelope from reviewed standard content schemas."""
import argparse
import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def build():
    schemas = json.loads((ROOT / "openapi/partner.v1.json").read_text(encoding="utf-8"))["components"]["schemas"]
    block = copy.deepcopy(schemas["Block"])
    block["properties"].pop("revision_id")
    block["required"].remove("revision_id")
    block["properties"]["order"] = {"type": "integer", "minimum": 0}
    block["required"].append("order")
    schemas["FrozenBlock"] = block
    definitions, pending = {}, ["FrozenBlock", "BlobRef"]

    def bundle(value):
        if isinstance(value, dict):
            result = {key: bundle(item) for key, item in value.items()}
            if "$ref" in result:
                name = result["$ref"].split("/")[-1]
                result["$ref"] = "#/$defs/" + name
                pending.append(name)
            return result
        if isinstance(value, list):
            return [bundle(item) for item in value]
        return value

    while pending:
        name = pending.pop()
        if name not in definitions:
            definitions[name] = bundle(schemas[name])
    return {"$schema": "https://json-schema.org/draft/2020-12/schema",
            "title": "AKOS frozen content envelope V1", "type": "object", "additionalProperties": False,
            "required": ["format", "blocks", "blobs"],
            "properties": {"format": {"const": "akos.freeze/1"},
                           "blocks": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/FrozenBlock"}},
                           "blobs": {"type": "array", "items": {"$ref": "#/$defs/BlobRef"}}},
            "$defs": dict(sorted(definitions.items()))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    path = ROOT / "schemas/frozen-content.v1.schema.json"
    content = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.check:
        if not path.exists() or path.read_text(encoding="utf-8") != content:
            raise SystemExit("Frozen content schema is stale")
        print("PASS: frozen schema matches standard content")
    else:
        path.write_text(content, encoding="utf-8")


if __name__ == "__main__":
    main()

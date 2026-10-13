"""Generate self-contained design tool schemas from the partner OpenAPI contract."""
import argparse
import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent
OUTPUT = ROOT / "mcp/tools.v1.json"


def bundle(root_schema, schemas):
    definitions = {}

    def convert(value):
        if isinstance(value, list):
            return [convert(item) for item in value]
        if not isinstance(value, dict):
            return value
        result = {}
        for key, item in value.items():
            if key != "$ref":
                result[key] = convert(item)
                continue
            prefix = "#/components/schemas/"
            if not item.startswith(prefix):
                raise ValueError(f"Unbundled reference: {item}")
            name = item[len(prefix):]
            if name not in definitions:
                definitions[name] = {}  # Reserve for recursive schema references.
                definitions[name] = convert(schemas[name])
            result[key] = f"#/$defs/{name}"
        return result

    result = convert(root_schema)
    result["$schema"] = "https://json-schema.org/draft/2020-12/schema"
    if definitions:
        result["$defs"] = {name: definitions[name] for name in sorted(definitions)}
    return result


def generate():
    spec = json.loads((ROOT / "openapi/partner.v1.json").read_text(encoding="utf-8"))
    schemas = spec["components"]["schemas"]
    tools = []
    for path, item in spec["paths"].items():
        operation = item["post"]
        request_ref = operation["requestBody"]["content"]["application/json"]["schema"]["$ref"]
        request = copy.deepcopy(schemas[request_ref.rsplit("/", 1)[1]])
        if path == "/ask":
            request["properties"].pop("stream")
        result = operation["responses"]["200"]["content"]["application/json"]["schema"]
        output = {"type": "object", "oneOf": [result, {"$ref": "#/components/schemas/Error"}]}
        tools.append({
            "name": operation["operationId"], "description": operation["summary"],
            "inputSchema": bundle(request, schemas), "outputSchema": bundle(output, schemas),
            "annotations": {"readOnlyHint": path != "/feedback", "destructiveHint": False,
                            "idempotentHint": True},
            "execution": {"taskSupport": "forbidden"},
        })
    return {"tools": tools}


def serialized():
    # A compact line per tool keeps generated schemas reviewable without duplication noise.
    tools = generate()["tools"]
    return '{\n  "tools": [\n' + ',\n'.join('    ' + json.dumps(tool, ensure_ascii=False, sort_keys=True) for tool in tools) + '\n  ]\n}\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Verify generated artifact is current without writing")
    args = parser.parse_args()
    expected = serialized()
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text(encoding="utf-8") != expected:
            raise SystemExit("MCP tool contract missing or stale; regenerate after reviewing OpenAPI changes")
        print("PASS: generated MCP catalogue matches partner OpenAPI")
    else:
        OUTPUT.parent.mkdir(parents=True, exist_ok=True)
        OUTPUT.write_text(expected, encoding="utf-8")
        print("Generated design catalogue: " + str(OUTPUT))


if __name__ == "__main__":
    main()

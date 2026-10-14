"""Closed dataset/run schemas for evidence Recall evaluation V1."""
import argparse
import json
from pathlib import Path


def build():
    ident = {"type": "string", "minLength": 1, "maxLength": 200}
    digest = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
    integer = {"type": "integer", "minimum": 0, "maximum": 9223372036854775807}
    def obj(props):
        return {"type": "object", "additionalProperties": False, "required": list(props), "properties": props}
    anchor = obj({"document_id": ident, "revision_id": ident, "block_id": ident, "start": integer, "end": integer})
    context = obj({"query_text": {"type": "string", "minLength": 1, "maxLength": 32768}, "subject_id": ident,
                   "policy_snapshot_digest": digest, "publication_snapshot_digest": digest})
    case = obj({"case_id": ident, "kind": {"enum": ["answerable", "unanswerable", "denied", "withdrawn"]},
                "request": context, "allowed": {"type": "array", "items": anchor}, "relevant": {"type": "array", "items": anchor}})
    item = obj({"item_id": ident, "fragments": {"type": "array", "minItems": 1, "items": anchor}})
    result = obj({"case_id": ident, "request": context, "status": {"enum": ["ok", "error", "timeout"]},
                  "items": {"type": "array", "maxItems": 10, "items": item}})
    common = {"dataset_id": ident, "corpus_digest": digest, "annotation_digest": digest}
    return {"retrieval-dataset.v1.schema.json": {"$schema": "https://json-schema.org/draft/2020-12/schema",
             "title": "AI Knowledge OS evidence retrieval dataset V1", **obj({**common, "cases": {"type": "array", "minItems": 1, "items": case}})},
            "retrieval-run.v1.schema.json": {"$schema": "https://json-schema.org/draft/2020-12/schema",
             "title": "AI Knowledge OS evidence retrieval run V1", **obj({**common, "profile_digest": digest,
             "implementation_id": ident, "results": {"type": "array", "items": result}})}}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__); parser.add_argument("--write", action="store_true"); args = parser.parse_args()
    for name, schema in build().items():
        path = Path(__file__).parent / "schemas" / name
        text = json.dumps(schema, ensure_ascii=False, indent=2) + "\n"
        if args.write: path.write_text(text, encoding="utf-8")
        elif path.read_text(encoding="utf-8") != text: raise SystemExit(name + " is stale")
    if not args.write: print("PASS: retrieval dataset and run schema generation consistency")

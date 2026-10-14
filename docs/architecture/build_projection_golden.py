"""Fixed-domain projection digest vectors, not verified publication facts."""
import argparse
import copy
import json
from pathlib import Path
from canonical_content import canonical
from publication_projection import projection_digest


def record():
    r = {"schema_version": 1, "policy_version": 9007199254740993, "expires_at_unix_ns": 1900000000000000001,
         "channels": ["keyword", "vector"], "entries": []}
    for key in ("projection_id", "request_id", "invocation_id", "parent_scope_id", "child_scope_id", "space_id", "knowledge_base_id", "backend_epoch_id", "route_id", "module_instance_id"):
        r[key] = key + "中文😀"
    for key in ("parent_publication_digest", "query_profile_digest", "pipeline_digest"):
        r[key] = "a" * 64
    for channel in r["channels"]:
        r["entries"].append({"document_id": "doc", "revision_id": "rev", "publication_version": 18446744073709551615,
            "generation_set_id": "set", "generation_id": channel + "-gen", "channel": channel, "index_profile_digest": "b" * 64})
    return r


def build():
    base = record()
    cases = [("unicode_large_integers", json.dumps(base, ensure_ascii=False)),
             ("key_order_whitespace", json.dumps(dict(reversed(list(base.items()))), ensure_ascii=False, indent=2))]
    for name, key, value in (("changed_invocation", "invocation_id", "another-invocation"),
                             ("changed_expiry", "expires_at_unix_ns", 1900000000000000002),
                             ("changed_parent", "parent_publication_digest", "b"*64)):
        r = copy.deepcopy(base); r[key] = value
        cases.append((name, json.dumps(r, ensure_ascii=False)))
    return {"format": "akos.publication-projection-golden/1", "encoding": "AKOS-CJSON/1", "domain": "akos.publication-projection/1",
            "cases": [{"name": name, "input_json": raw, "canonical_json": canonical(raw.encode()).decode(), "digest": projection_digest(raw.encode())} for name, raw in cases]}


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__); p.add_argument("--write", action="store_true"); args = p.parse_args()
    path = Path(__file__).parent / "examples/publication-projection-golden.v1.json"
    text = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.write: path.write_text(text, encoding="utf-8")
    elif path.read_text(encoding="utf-8") != text: raise SystemExit("projection digest golden vectors are stale")
    else: print("PASS: five projection digest vectors")

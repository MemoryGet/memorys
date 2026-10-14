"""Draft offline bound-probe review, not a remote authenticated protocol."""
import argparse
import json
from pathlib import Path
from build_store_binding import build as store_schema


def build():
    old = store_schema()["properties"]
    expected = old["expectation"]
    observation = old["observations"]["items"]
    endpoint = expected["properties"]["endpoints"]["items"]
    text = {"type": "string", "minLength": 1, "maxLength": 4096}
    version = {"type": "integer", "minimum": 1, "maximum": 18446744073709551615}
    ns = {"type": "integer", "minimum": 1, "maximum": 9223372036854775807}
    digest = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
    def obj(p): return {"type": "object", "additionalProperties": False, "required": list(p), "properties": p}
    fields = {k: text for k in ("check_id", "probe_id", "invocation_id", "request_id", "binding_id", "control_domain_id", "scope_id", "subject_id")}
    fields.update(binding_version=version, policy_version=version, expected_binding_digest=digest, request_profile_digest=digest,
                  endpoint=endpoint, issued_at_ns=ns, deadline_ns=ns)
    binding = obj(fields)
    request = obj({"request": obj({"request_id": text, "profile_digest": digest, "deadline_ns": ns}),
                   "scope": obj({"id": text, "subject_id": text, "domain_id": text, "action": {"const": "metadata.store.observe"}, "policy_version": version, "expires_at_ns": ns}),
                   "binding": binding})
    return {"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "akos.store-probe-review/1",
            **obj({"schema_version": {"type": "integer", "const": 1}, "expectation": expected, "request": request,
                   "observation": obj({"binding": binding, "observation": observation, "observed_at_ns": ns})})}


if __name__ == "__main__":
    p=argparse.ArgumentParser(description=__doc__);p.add_argument("--write",action="store_true");a=p.parse_args()
    path=Path(__file__).parent/'schemas/store-probe-review.v1.schema.json'
    text=json.dumps(build(),ensure_ascii=False,indent=2)+'\n'
    if a.write:path.write_text(text,encoding='utf-8')
    elif path.read_text(encoding='utf-8')!=text:raise SystemExit('store probe review schema is stale')
    else:print('PASS: store probe review schema generation consistency')

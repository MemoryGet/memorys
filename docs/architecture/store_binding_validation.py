"""Identity inventory checks; provenance/coverage/readiness always unverified."""
import json
from jsonschema import Draft202012Validator
from build_store_binding import build


def validate_bundle(bundle):
    try:
        Draft202012Validator(build()).validate(bundle)
        if type(bundle["schema_version"]) is not int: raise ValueError()
        def strings(value):
            if isinstance(value, str):
                if len(value.encode("utf-8", errors="strict")) > 4096: raise ValueError()
            elif isinstance(value, dict):
                for k, v in value.items(): strings(k); strings(v)
            elif isinstance(value, list):
                for v in value: strings(v)
        strings(bundle)
        expected, observations = bundle["expectation"], bundle["observations"]
        for identity in [expected["identity"], *[o["identity"] for o in observations]]:
            if any(type(identity[k]) is not int for k in ("binding_version", "metadata_model_version")): raise ValueError()
        endpoints = {}
        for e in expected["endpoints"]:
            if e["role"] in endpoints: raise ValueError()
            endpoints[e["role"]] = e
        if not {"metadata_uow", "query_read_unit"}.issubset(endpoints) or len(observations) != len(endpoints): raise ValueError()
        roles, receipts = set(), set()
        for o in observations:
            role = o["endpoint"]["role"]
            if role in roles or o["observation_id"] in receipts or endpoints.get(role) != o["endpoint"] or o["binding_id"] != expected["binding_id"] or o["identity"] != expected["identity"]: raise ValueError()
            roles.add(role); receipts.add(o["observation_id"])
        return {"matched_observations": len(observations), "provenance_verified": False, "coverage_verified": False, "ready_for_service": False}
    except Exception:
        raise ValueError("invalid_store_binding_review") from None


def load_bundle(raw):
    if not isinstance(raw, bytes) or len(raw) > 4 << 20: raise ValueError("invalid_store_binding_review")
    def pairs(items):
        out = {}
        for k, v in items:
            if k in out: raise ValueError()
            out[k] = v
        return out
    def reject(_): raise ValueError()
    try:
        bundle = json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_float=reject, parse_constant=reject)
        validate_bundle(bundle)
        return bundle
    except Exception:
        raise ValueError("invalid_store_binding_review") from None

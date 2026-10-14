"""Structural checks only; no current policy/publication or provenance check."""
import json
import hashlib
from canonical_content import canonical
from jsonschema import Draft202012Validator
from build_publication_projection import build


def validate_projection(record):
    try:
        Draft202012Validator(build()).validate(record)
        for value in (record["schema_version"], record["policy_version"], record["expires_at_unix_ns"]):
            if type(value) is not int:
                raise ValueError()
        def unicode_check(value):
            if isinstance(value, str):
                value.encode("utf-8", errors="strict")
            elif isinstance(value, dict):
                for k, v in value.items():
                    unicode_check(k); unicode_check(v)
            elif isinstance(value, list):
                for item in value: unicode_check(item)
        unicode_check(record)
        order = {"keyword": 0, "vector": 1, "graph": 2}
        if record["channels"] != sorted(record["channels"], key=order.get):
            raise ValueError()
        entries = record["entries"]
        def key(entry):
            return tuple(entry[k].encode("utf-8") for k in ("document_id", "revision_id", "channel", "generation_id"))
        if entries != sorted(entries, key=key):
            raise ValueError()
        generations, documents, revisions, sets, document_channels = set(), {}, {}, {}, set()
        for entry in entries:
            if type(entry["publication_version"]) is not int or entry["generation_id"] in generations:
                raise ValueError()
            generations.add(entry["generation_id"])
            doc, rev = entry["document_id"], entry["revision_id"]
            identity = (rev, entry["publication_version"], entry["generation_set_id"])
            if doc in documents and documents[doc] != identity:
                raise ValueError()
            documents[doc] = identity
            if rev in revisions and revisions[rev] != doc:
                raise ValueError()
            revisions[rev] = doc
            set_identity = (doc, rev, entry["index_profile_digest"])
            sid = entry["generation_set_id"]
            if sid in sets and sets[sid] != set_identity:
                raise ValueError()
            sets[sid] = set_identity
            pair = (doc, entry["channel"])
            if pair in document_channels:
                raise ValueError()
            document_channels.add(pair)
        if {e["channel"] for e in entries} != set(record["channels"]):
            raise ValueError()
        return {"entry_count": len(entries), "revision_count": len(revisions), "authorized": False, "ready_for_retrieval": False}
    except Exception:
        raise ValueError("invalid_publication_projection") from None


def load_projection(raw):
    if not isinstance(raw, bytes) or len(raw) > 4 << 20:
        raise ValueError("invalid_publication_projection")
    def pairs(items):
        out = {}
        for k, v in items:
            if k in out: raise ValueError()
            out[k] = v
        return out
    def reject(_): raise ValueError()
    try:
        record = json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_float=reject, parse_constant=reject)
        validate_projection(record)
        return record
    except Exception:
        raise ValueError("invalid_publication_projection") from None


def projection_digest(raw):
    # Structural validation precedes canonical hashing; neither proves source.
    load_projection(raw)
    return hashlib.sha256(b"akos.publication-projection/1\x00" + canonical(raw)).hexdigest()

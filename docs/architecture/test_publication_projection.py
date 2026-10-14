import copy
import json
import unittest
from publication_projection import load_projection, validate_projection


def fixture():
    r = {"schema_version": 1, "policy_version": 9007199254740993, "expires_at_unix_ns": 1900000000000000001,
         "channels": ["keyword", "vector"], "entries": []}
    for k in ("projection_id", "request_id", "invocation_id", "parent_scope_id", "child_scope_id", "space_id", "knowledge_base_id", "backend_epoch_id", "route_id", "module_instance_id"):
        r[k] = k + "中文"
    for k in ("parent_publication_digest", "query_profile_digest", "pipeline_digest"):
        r[k] = "a" * 64
    for channel in r["channels"]:
        r["entries"].append({"document_id": "doc", "revision_id": "rev", "publication_version": 18446744073709551615,
            "generation_set_id": "set", "generation_id": channel + "-gen", "channel": channel, "index_profile_digest": "b" * 64})
    return r


class ProjectionTests(unittest.TestCase):
    def reject(self, change):
        r = fixture(); change(r)
        with self.assertRaisesRegex(ValueError, "invalid_publication_projection"): validate_projection(r)

    def test_precise_integers_and_no_authorization_claim(self):
        r = fixture(); actual = load_projection(json.dumps(r, ensure_ascii=False).encode())
        self.assertEqual(actual, r)
        report = validate_projection(actual)
        self.assertEqual(report["entry_count"], 2)
        self.assertFalse(report["authorized"]); self.assertFalse(report["ready_for_retrieval"])

    def test_closed_fields_and_integer_types(self):
        for mutate in (lambda r: r.update(extra=1), lambda r: r.pop("child_scope_id"), lambda r: r.update(schema_version=True), lambda r: r.update(policy_version=1.0), lambda r: r["entries"][0].update(publication_version=1.0)):
            self.reject(mutate)

    def test_duplicate_generation_and_old_revision_rejected(self):
        self.reject(lambda r: r["entries"][1].update(generation_id=r["entries"][0]["generation_id"]))
        self.reject(lambda r: r["entries"][1].update(revision_id="rev-old"))
        self.reject(lambda r: r["entries"][1].update(publication_version=2))

    def test_channel_coverage_and_order(self):
        self.reject(lambda r: r["channels"].reverse())
        self.reject(lambda r: r["entries"].reverse())
        self.reject(lambda r: r["channels"].append("graph"))
        self.reject(lambda r: r["entries"][1].update(channel="keyword"))

    def test_generation_set_identity_and_document_owner(self):
        self.reject(lambda r: r["entries"][1].update(index_profile_digest="c" * 64))
        self.reject(lambda r: r["entries"][1].update(generation_set_id="other-set"))
        self.reject(lambda r: r["entries"][1].update(document_id="other-doc"))

    def test_raw_duplicates_floats_surrogates_and_size(self):
        raw = json.dumps(fixture()).encode()
        for invalid in (raw.replace(b'"schema_version": 1', b'"schema_version": 1, "schema_version": 1'), raw.replace(b'"schema_version": 1', b'"schema_version": 1.0'), b'{"x":"\\ud800"}', b'x' * ((4 << 20) + 1)):
            with self.assertRaises(ValueError): load_projection(invalid)
        self.reject(lambda r: r.update(projection_id="\ud800"))


if __name__ == "__main__": unittest.main()

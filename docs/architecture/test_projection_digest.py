import json
import copy
import subprocess
import tempfile
import unittest
from pathlib import Path
from build_projection_golden import build, record
from publication_projection import projection_digest


class ProjectionDigestTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.root = Path(__file__).resolve().parents[2]
        cls.temp = tempfile.TemporaryDirectory()
        cls.exe = Path(cls.temp.name) / "projection-digest.exe"
        result = subprocess.run([str(cls.root / ".tools/go/bin/go.exe"), "build", "-o", str(cls.exe), "./cmd/architecture-projection-digest"], cwd=cls.root, capture_output=True)
        if result.returncode: raise RuntimeError("Go digest inspector build failed")

    @classmethod
    def tearDownClass(cls): cls.temp.cleanup()

    def test_goldens_actual_go_and_python(self):
        generated = build()
        path = Path(__file__).parent / "examples/publication-projection-golden.v1.json"
        self.assertEqual(json.loads(path.read_text(encoding="utf-8")), generated)
        for case in generated["cases"]:
            result = subprocess.run([str(self.exe)], input=case["input_json"].encode(), capture_output=True)
            self.assertEqual(result.returncode, 0)
            actual = json.loads(result.stdout)
            self.assertEqual(actual["digest"], case["digest"])
            self.assertFalse(actual["schema_validated"]); self.assertFalse(actual["authorized"])

    def test_stable_order_and_bound_fields(self):
        cases = build()["cases"]
        self.assertEqual(cases[0]["digest"], cases[1]["digest"])
        self.assertEqual(len({c["digest"] for c in cases}), 4)

    def test_structural_failures_do_not_get_validated_digest(self):
        r = record(); r["entries"].reverse()
        with self.assertRaises(ValueError): projection_digest(json.dumps(r).encode())
        r = record(); r["policy_version"] = 1.0
        with self.assertRaises(ValueError): projection_digest(json.dumps(r).encode())

    def test_go_helper_is_explicitly_unchecked_and_rejects_bad_json(self):
        result = subprocess.run([str(self.exe)], input=b'{}', capture_output=True)
        self.assertEqual(result.returncode, 0)
        self.assertFalse(json.loads(result.stdout)["schema_validated"])
        for raw in (b'[]', b'{"a":1,"a":2}', b'{}{}', b'{"x":"\\ud800"}'):
            self.assertNotEqual(subprocess.run([str(self.exe)], input=raw, capture_output=True).returncode, 0)

    def test_validated_go_matches_python_goldens(self):
        for case in build()["cases"]:
            result = subprocess.run([str(self.exe), "--validate"], input=case["input_json"].encode(), capture_output=True)
            self.assertEqual(result.returncode, 0, case["name"])
            actual = json.loads(result.stdout)
            self.assertEqual(actual["digest"], case["digest"])
            self.assertTrue(actual["schema_validated"]); self.assertFalse(actual["authorized"])

    def test_validated_go_rejects_relationships_and_closed_keys(self):
        mutations = [lambda r: r.update(policy_version=1.0), lambda r: r.update(policy_version=True),
                     lambda r: r.update(POLICY_VERSION=r.pop("policy_version")), lambda r: r.update(extra=1),
                     lambda r: r["entries"].reverse(), lambda r: r["channels"].append("graph"),
                     lambda r: r["entries"][1].update(revision_id="other"),
                     lambda r: r["entries"][1].update(index_profile_digest="c"*64),
                     lambda r: r["entries"][1].update(generation_id=r["entries"][0]["generation_id"]),
                     lambda r: r["entries"][0].update(Generation_ID=r["entries"][0].pop("generation_id"))]
        for mutate in mutations:
            r = copy.deepcopy(record()); mutate(r); raw = json.dumps(r).encode()
            with self.assertRaises(ValueError): projection_digest(raw)
            self.assertNotEqual(subprocess.run([str(self.exe), "--validate"], input=raw, capture_output=True).returncode, 0)

    def test_validated_go_integer_limits_null_and_unicode(self):
        for mutate in (lambda r: r.update(policy_version=18446744073709551616),
                       lambda r: r.update(expires_at_unix_ns=9223372036854775808),
                       lambda r: r.update(parent_scope_id=None), lambda r: r.update(projection_id="\ud800"),
                       lambda r: r["entries"][0].update(publication_version=0)):
            r = record(); mutate(r); raw = json.dumps(r).encode()
            with self.assertRaises(ValueError): projection_digest(raw)
            self.assertNotEqual(subprocess.run([str(self.exe), "--validate"], input=raw, capture_output=True).returncode, 0)


if __name__ == "__main__": unittest.main()

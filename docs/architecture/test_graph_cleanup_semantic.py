import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from jsonschema import Draft202012Validator, ValidationError
from graph_cleanup_semantic import golden, schema, request_digest

ROOT=Path(__file__).resolve().parent


class GraphCleanupSemanticTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();cls.addClassCleanup(cls.temp.cleanup)
        cls.exe=Path(cls.temp.name)/"graph-cleanup.exe"
        project=ROOT.parents[1]
        subprocess.run([str(project/".tools/go/bin/go.exe"),"build","-o",str(cls.exe),"./cmd/architecture-graph-cleanup-digest"],cwd=project,check=True,capture_output=True,timeout=60)

    def invoke(self,raw):return subprocess.run([str(self.exe)],input=raw,capture_output=True,timeout=10)

    def test_schema_and_independent_golden(self):
        Draft202012Validator.check_schema(schema())
        self.assertEqual(json.loads((ROOT/"schemas/graph-cleanup-semantic.v1.schema.json").read_text()),schema())
        self.assertEqual(json.loads((ROOT/"examples/graph-cleanup-golden.v1.json").read_text(encoding="utf-8")),golden())
        for case in golden()["cases"]:
            result=self.invoke(case["input_json"].encode())
            self.assertEqual(result.returncode,0,result.stderr)
            report=json.loads(result.stdout)
            self.assertEqual(report["digest"],case["sha256"])
            self.assertFalse(report["source_authenticated"])

    def test_property_order_and_whitespace_do_not_change_hash(self):
        for case in golden()["cases"]:
            raw=json.dumps(json.loads(case["input_json"]),ensure_ascii=False,indent=3,sort_keys=True).encode()
            self.assertEqual(request_digest(raw),case["sha256"])
            self.assertEqual(json.loads(self.invoke(raw).stdout)["digest"],case["sha256"])

    def test_both_reject_ambiguous_and_mixed_variant_inputs(self):
        source=golden()["cases"][0]["input_json"].encode()
        deletion=golden()["cases"][1]["input_json"].encode()
        cases=[source.replace(b'"publication_epoch": 18446744073709551615',b'"publication_epoch": 1.0'),
               source.replace(b'"operation_id": "operation"',b'"operation_id":"operation","operation_id":"other"'),
               source.replace(b'"kind": "source_invalidation"',b'"kind":"source_invalidation","retention_permit_id":""'),
               deletion.replace(b'"kind": "generation_deletion"',b'"kind":"generation_deletion","publication_epoch":0'),
               source.replace(b'"schema_version": "1.0"',b'"schema_version":"2.0"')]
        for raw in cases:
            with self.assertRaises((ValueError, ValidationError, UnicodeError)):request_digest(raw)
            self.assertNotEqual(self.invoke(raw).returncode,0)

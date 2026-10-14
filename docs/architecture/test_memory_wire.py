import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from jsonschema import ValidationError

from memory_record_validation import validate
from test_memory_record import record


class MemoryWireTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.root = Path(__file__).resolve().parents[2]
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.exe = Path(cls.temp.name) / "memory-wire.exe"
        subprocess.run([str(cls.root / ".tools/go/bin/go.exe"), "build", "-o", str(cls.exe),
                        "./cmd/architecture-memory-record"], cwd=cls.root, check=True,
                       capture_output=True, timeout=60)

    def invoke(self, raw):
        return subprocess.run([str(self.exe)], input=raw, capture_output=True, timeout=10)

    def test_actual_go_roundtrip_matches_python_schema(self):
        samples = [record()]
        task = record(); task["task_id"] = "task"
        task["provenance"] = {"kind": "task_experience", "task_id": "task", "citations": []}
        samples.append(task)
        for sample in samples:
            sample["version"] = 18446744073709551615
            sample["expires_at"] = "2026-10-13T00:00:00.000000002Z"
            validate(sample)
            result = self.invoke(json.dumps(sample, ensure_ascii=False).encode())
            self.assertEqual(result.returncode, 0, result.stderr)
            out = json.loads(result.stdout)
            validate(out)
            self.assertEqual(out, sample)

    def test_actual_go_rejects_closed_shape_raw_duplicates_and_floats(self):
        raw = json.dumps(record(), ensure_ascii=False).encode()
        cases = [raw.replace(b'"version": 9007199254740993', b'"version": 1.0'),
                 raw.replace(b'"id": "memory"', b'"id": "memory", "id": "other"'),
                 raw.replace(b'"owner_id": "user"', b'"owner_id": null'),
                 raw.replace(b'"schema_version": "1.0"', b'"schema_version": "2.0"')]
        bad = copy.deepcopy(record()); bad["provenance"].pop("confirmation_id")
        cases.append(json.dumps(bad).encode())
        for candidate in cases:
            self.assertNotEqual(self.invoke(candidate).returncode, 0)

    def test_locator_and_time_rejections_agree(self):
        base = record()
        base["provenance"] = {"kind": "knowledge_summary", "citations": [{
            "document_id": "doc", "revision_id": "rev", "block_id": "block",
            "source_anchor": {"kind": "original", "blob_id": "blob", "locator": {
                "precision": "exact", "page": 1, "char_start": 0, "char_end": 2,
                "bbox": [0.1, 0.2, 0.8, 0.9]}}}]}
        for name, change in [
            ("zero-created", lambda r,l: r.update(created_at="0001-01-01T00:00:00Z")),
            ("reversed-character-range", lambda r,l: l.update(char_end=0,char_start=2)),
            ("reversed-box", lambda r,l: l.update(bbox=[0.8,0.2,0.1,0.9])),
            ("empty-worksheet", lambda r,l: l.update(worksheet="")),
            ("empty-cell-range", lambda r,l: l.update(worksheet="sheet",cell_range="")),
            ("empty-paragraph-path", lambda r,l: l.update(paragraph_path=[])),
            ("bbox-no-page", lambda r,l: l.pop("page")),
            ("negative-page", lambda r,l: l.update(page=-1)),
        ]:
            with self.subTest(name=name):
                candidate = copy.deepcopy(base)
                locator = candidate["provenance"]["citations"][0]["source_anchor"]["locator"]
                change(candidate,locator)
                try:
                    validate(candidate)
                    python_accepts = True
                except (ValueError, UnicodeError, ValidationError):
                    python_accepts = False
                go_accepts = self.invoke(json.dumps(candidate).encode()).returncode == 0
                self.assertFalse(python_accepts, "Python accepted invalid locator/time")
                self.assertFalse(go_accepts, "Go accepted invalid locator/time")

    def test_full_citation_and_optional_fields_roundtrip(self):
        sample = record()
        sample.update(kb_id="kb", task_id="task", expires_at="9999-12-31T23:59:59.999999999Z")
        sample["provenance"] = {"kind":"knowledge_summary", "citations":[{
            "document_id":"doc", "revision_id":"rev", "block_id":"block",
            "source_anchor":{"kind":"revision", "revision_block_id":"block",
                             "locator":{"precision":"exact", "worksheet":"工作表", "cell_range":"A1:B2",
                                        "paragraph_path":[0,1], "char_start":0, "char_end":2},
                             "derived_from":[{"blob_id":"blob", "locator":{"precision":"exact", "page":1,
                                               "bbox":[0.1,0.2,0.8,0.9]}}]}}]}
        validate(sample)
        result = self.invoke(json.dumps(sample,ensure_ascii=False).encode())
        self.assertEqual(result.returncode,0,result.stderr)
        out = json.loads(result.stdout)
        validate(out)
        self.assertEqual(out,sample)

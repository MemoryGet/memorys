import copy
import json
from pathlib import Path
import unittest
from jsonschema import Draft202012Validator, ValidationError

from build_memory_record import build
from memory_record_validation import validate


def record():
    return {"schema_version": "1.0", "id": "memory", "owner_id": "user", "space_id": "space",
            "content": "用户确认偏好", "provenance": {"kind": "user_confirmed", "confirmation_id": "confirmation", "citations": []},
            "created_at": "2026-10-13T00:00:00.000000001Z", "version": 9007199254740993}


class MemoryRecordTests(unittest.TestCase):
    def test_saved_schema_and_large_version(self):
        Draft202012Validator.check_schema(build())
        self.assertEqual(json.loads((Path(__file__).parent / "schemas/memory-record.v1.schema.json").read_text(encoding="utf-8")), build())
        report = validate(record())
        self.assertTrue(report["shape_valid"])
        self.assertFalse(report["source_authenticated"])

    def test_missing_mixed_and_unknown_sources(self):
        for change in [lambda r: r["provenance"].pop("confirmation_id"),
                       lambda r: r["provenance"].update(task_id="task"),
                       lambda r: r["provenance"].update(kind="model_inferred"),
                       lambda r: r["provenance"].update(cause="supplier detail"),
                       lambda r: r.update(version=1.0),
                       lambda r: r.update(version=True),
                       lambda r: r.update(content="\ud800")]:
            bad = copy.deepcopy(record()); change(bad)
            with self.assertRaises((ValidationError, ValueError, UnicodeError)):
                validate(bad)

    def test_expiry_is_exact_to_nanoseconds(self):
        r = record(); r["expires_at"] = "2026-10-13T00:00:00.000000002Z"
        validate(r)
        for expiry in [r["created_at"], "2026-10-13T00:00:00Z", "2026-02-30T00:00:00Z"]:
            r["expires_at"] = expiry
            with self.assertRaises((ValidationError, ValueError)):
                validate(r)

    def test_task_scope_and_revision_ownership(self):
        r = record(); r["provenance"] = {"kind": "task_experience", "task_id": "task", "citations": []}
        with self.assertRaises(ValueError): validate(r)
        r["task_id"] = "task"; validate(r)
        citation = {"document_id": "doc", "revision_id": "rev", "block_id": "block",
                    "source_anchor": {"kind": "revision", "revision_block_id": "block", "locator": {"precision": "coarse"}}}
        r["provenance"] = {"kind": "knowledge_summary", "citations": [citation]}
        validate(r)
        r["provenance"]["citations"].append({**copy.deepcopy(citation), "document_id": "other"})
        with self.assertRaises(ValueError): validate(r)

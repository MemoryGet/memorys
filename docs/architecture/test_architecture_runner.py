import sys
import tempfile
import unittest
from pathlib import Path

from check_architecture import execute, source_inventory


class ArchitectureRunnerTests(unittest.TestCase):
    def test_exit_status_and_diagnostics_are_preserved(self):
        ok = execute([sys.executable, "-c", "print('probe passed')"], 5)
        self.assertEqual(ok["status"], "passed")
        self.assertIn("probe passed", ok["stdout"])
        failed = execute([sys.executable, "-c",
                          "import sys; print('deliberate failure', file=sys.stderr); sys.exit(7)"], 5)
        self.assertEqual(failed["status"], "failed")
        self.assertEqual(failed["exit_code"], 7)
        self.assertIn("deliberate failure", failed["stderr"])

    def test_missing_executable_does_not_skip_or_pass(self):
        with tempfile.TemporaryDirectory() as folder:
            result = execute([str(Path(folder) / "missing-tool.exe")], 5)
        self.assertEqual(result["status"], "failed")
        self.assertIsNone(result["exit_code"])
        self.assertEqual(result["error"], "FileNotFoundError")

    def test_timeout_is_failure(self):
        result = execute([sys.executable, "-c", "import time; time.sleep(5)"], 0.05)
        self.assertEqual(result["status"], "failed")
        self.assertEqual(result["error"], "TimeoutExpired")
        self.assertIsNone(result["exit_code"])

    def test_inventory_covers_binary_fixtures_and_reviewed_lock(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            inputs = {"docs/architecture/sql/contract.sql": b"SELECT 1;",
                      "pkg/example/testdata/table.pdf": b"pdf sample",
                      "docs/architecture/examples/sample.bin": b"binary sample",
                      "docs/architecture/evidence/openapi-validation-requirements.txt": b"validator==0.9.0"}
            for name, data in inputs.items():
                p = root / name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_bytes(data)
            report = root / "docs/architecture/evidence/unified-local-checks.json"
            report.write_text("{}")
            cache = root / "docs/architecture/__pycache__/ignored.pyc"
            cache.parent.mkdir(parents=True)
            cache.write_bytes(b"cache")
            before = source_inventory(root)
            self.assertEqual(set(before), set(inputs))
            report.write_text('{"result":"changed"}')
            self.assertEqual(source_inventory(root), before)
            (root / next(iter(inputs))).write_bytes(b"changed input")
            self.assertNotEqual(source_inventory(root), before)

    def test_inventory_detects_added_and_removed_inputs(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            before = source_inventory(root)
            p = root / "cmd/new/main.go"
            p.parent.mkdir(parents=True)
            p.write_text("package main")
            added = source_inventory(root)
            self.assertNotEqual(added, before)
            p.unlink()
            self.assertEqual(source_inventory(root), before)


if __name__ == "__main__":
    unittest.main()

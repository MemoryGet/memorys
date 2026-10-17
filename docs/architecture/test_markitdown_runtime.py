"""Probe lifecycle/evidence checks, independent of optional converter dependencies."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check_markitdown_runtime as probe


class MarkItDownProbeTests(unittest.TestCase):
    def invoke(self, folder, go_evidence=None, failure=None, changed=False):
        inventory = {"source": "before"}
        snapshots = [inventory, {"source": "after"} if changed else inventory]

        def run(command, **kwargs):
            if "build" in command:
                Path(command[command.index("-o") + 1]).write_bytes(b"probe executable")
                if failure == "build":
                    raise subprocess.CalledProcessError(1, command)
                return subprocess.CompletedProcess(command, 0)
            if "-I" in command:
                if failure == "timeout":
                    raise subprocess.TimeoutExpired(command, 45)
                return subprocess.CompletedProcess(command, 0, '{"version":"markitdown/0.1.3","markdown":"expected"}', "")
            return subprocess.CompletedProcess(command, 0, json.dumps(go_evidence), "")

        evidence = {"passed": True, "production_acceptance": False, "configured_factory": True,
                    "closed_reference_rejected": True, "closed_projector_rejected": True,
                    "original_quote_verified": True, "invented_quote_rejected": True, "format": "pdf"}
        if go_evidence is None:
            go_evidence = evidence
        with patch.object(probe, "ROOT", folder), patch.object(probe, "fixtures", return_value={"pdf": (b"source", "expected")}), \
                patch.object(probe.subprocess, "run", side_effect=run), \
                patch("check_architecture.source_inventory", side_effect=snapshots), \
                patch("importlib.metadata.version", return_value="0.1.3"):
            return probe.run_probe()

    def root(self, directory):
        folder = Path(directory)
        (folder / "tools/parsers").mkdir(parents=True)
        (folder / "tools/parsers/markitdown_worker.py").write_bytes(b"worker")
        (folder / "docs/architecture").mkdir(parents=True)
        (folder / "docs/architecture/markitdown-requirements.lock.txt").write_bytes(b"lock")
        return folder

    def test_private_build_and_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = self.root(directory)
            report = self.invoke(folder)
            self.assertTrue(report["passed"])
            self.assertTrue(report["sources_unchanged_during_checks"])
            self.assertEqual(len(report["probe_executable_sha256"]), 64)
            self.assertTrue(report["records"][0]["closed_reference_rejected"])
            self.assertTrue(report["records"][0]["original_quote_verified"])
            self.assertTrue(report["records"][0]["closed_projector_rejected"])
            self.assertEqual(list((folder / ".local/architecture-check").iterdir()), [])

    def test_build_timeout_and_false_evidence_clean_owned_resources(self):
        for failure, evidence in [("build", None), ("timeout", None), (None, {"passed": True})]:
            with self.subTest(failure=failure, evidence=evidence), tempfile.TemporaryDirectory() as directory:
                folder = self.root(directory)
                with self.assertRaises((RuntimeError, subprocess.SubprocessError)):
                    self.invoke(folder, evidence, failure)
                self.assertEqual(list((folder / ".local/architecture-check").iterdir()), [])

    def test_source_change_rejects_report(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = self.root(directory)
            with self.assertRaisesRegex(RuntimeError, "sources changed"):
                self.invoke(folder, changed=True)

    def test_missing_or_false_original_evidence_is_rejected(self):
        valid = {"passed": True, "production_acceptance": False, "configured_factory": True,
                 "closed_reference_rejected": True, "closed_projector_rejected": True,
                 "original_quote_verified": True, "invented_quote_rejected": True, "format": "pdf"}
        for key in ("closed_projector_rejected", "original_quote_verified", "invented_quote_rejected"):
            for missing in (True, False):
                evidence = dict(valid)
                if missing:
                    del evidence[key]
                else:
                    evidence[key] = False
                with self.subTest(key=key, missing=missing), tempfile.TemporaryDirectory() as directory:
                    folder = self.root(directory)
                    with self.assertRaisesRegex(RuntimeError, "assembly evidence incomplete"):
                        self.invoke(folder, evidence)
                    self.assertEqual(list((folder / ".local/architecture-check").iterdir()), [])

    def test_failed_run_replaces_previous_green_report(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "report.json"
            report.write_text('{"passed":true}', encoding="utf-8")
            with patch("sys.argv", ["probe", "--report", str(report)]), \
                    patch.object(probe, "run_probe", side_effect=RuntimeError("failure")):
                with self.assertRaises(RuntimeError):
                    probe.main()
            result = json.loads(report.read_text(encoding="utf-8"))
            self.assertFalse(result["passed"])
            self.assertEqual(result["status"], "failed")


if __name__ == "__main__":
    unittest.main()

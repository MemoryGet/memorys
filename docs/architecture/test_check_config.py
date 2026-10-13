"""File-level acceptance checks for the offline deployment inspection command."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from check_assembly_semantics import fixtures

ROOT = Path(__file__).resolve().parent


class CheckConfigTests(unittest.TestCase):
    def invoke(self, config, manifest):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "config.json").write_text(config, encoding="utf-8")
            (root / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            result = subprocess.run([sys.executable, str(ROOT / "check_config.py"),
                                     "--config", str(root / "config.json"),
                                     "--manifest", str(root / "manifest.json")],
                                    capture_output=True, text=True, timeout=10)
            return result

    def test_complete_offline_report_has_order_hashes_and_explicit_limits(self):
        config, providers = fixtures()
        result = self.invoke(json.dumps(config), next(iter(providers.values())))
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["status"], "offline_validated")
        self.assertEqual(report["creation_order"], ["design-fixture"])
        self.assertEqual(len(report["config_sha256"]), 64)
        self.assertFalse(report["ready_for_service"])
        self.assertIn("artifact_lock", report["unverified"])

    def test_duplicate_keys_and_private_values_are_rejected_safely(self):
        _, providers = fixtures()
        result = self.invoke('{"schema_version":1,"schema_version":2,"private":"secret-test-value"}',
                             next(iter(providers.values())))
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("secret-test-value", result.stderr + result.stdout)
        self.assertIn("duplicate", result.stderr)

    def test_invalid_config_schema_is_rejected(self):
        config, providers = fixtures()
        manifest = copy.deepcopy(next(iter(providers.values())))
        manifest["config_schema"] = {"type": 123}
        result = self.invoke(json.dumps(config), manifest)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("validation_failed", result.stderr)
        self.assertNotIn("Traceback", result.stderr)

    def test_dynamic_external_schema_reference_is_rejected(self):
        config, providers = fixtures()
        manifest = copy.deepcopy(next(iter(providers.values())))
        manifest["config_schema"]["$dynamicRef"] = "https://invalid.example/schema"
        result = self.invoke(json.dumps(config), manifest)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("reference", result.stderr)

    def test_malformed_and_unbounded_json_is_rejected(self):
        _, providers = fixtures()
        manifest = next(iter(providers.values()))
        for config in ["null", '{"x":NaN}', '{"x":1e9999}', '{"x":',
                       " " * ((4 << 20) + 1), '{"x":' + "[" * 65 + "0" + "]" * 65 + "}"]:
            with self.subTest(input_length=len(config)):
                result = self.invoke(config, manifest)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("validation_failed", result.stderr)
                self.assertNotIn("Traceback", result.stderr)

    def test_local_schema_references_work_but_missing_targets_fail(self):
        config, providers = fixtures()
        manifest = copy.deepcopy(next(iter(providers.values())))
        manifest["config_schema"] = {"$defs": {"settings": {"type": "object", "additionalProperties": False}},
                                      "$ref": "#/$defs/settings"}
        self.assertEqual(self.invoke(json.dumps(config), manifest).returncode, 0)
        manifest["config_schema"]["$ref"] = "#/$defs/missing"
        result = self.invoke(json.dumps(config), manifest)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("Traceback", result.stderr)

    def test_noncanonical_settings_do_not_leak_in_schema_error(self):
        config, providers = fixtures()
        config["modules"][0]["settings"]["secret"] = "secret-test-value"
        result = self.invoke(json.dumps(config), next(iter(providers.values())))
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("secret-test-value", result.stderr + result.stdout)

    def test_unused_broken_reference_fails_before_settings_evaluation(self):
        config, providers = fixtures()
        manifest = copy.deepcopy(next(iter(providers.values())))
        manifest["config_schema"]["$defs"] = {"unused": {"$ref": "#/$defs/missing"}}
        result = self.invoke(json.dumps(config), manifest)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("reference", result.stderr)


if __name__ == "__main__":
    unittest.main()

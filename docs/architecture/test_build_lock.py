"""Local synthetic artifacts prove input checks only, not module behavior."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from check_build_lock import inspect_lock
from check_config import ROOT


class BuildLockTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.config = self.root / "deployment.json"
        self.manifest = self.root / "manifest.json"
        self.retention = self.root / "retention.json"
        self.artifact = self.root / "artifact.bin"
        self.lock = self.root / "lock.json"
        for destination, source in ((self.config, "design-deployment.json"), (self.manifest, "design-provider-manifest.json"), (self.retention, "design-retention-policy.json")):
            destination.write_bytes((ROOT / "examples" / source).read_bytes())
        self.artifact.write_bytes(b"Synthetic artifact, no executable provider")
        config = json.loads(self.config.read_text(encoding="utf-8"))
        manifest = json.loads(self.manifest.read_text(encoding="utf-8"))
        policy = json.loads(self.retention.read_text(encoding="utf-8"))
        self.key = (manifest["module_id"], manifest["implementation_version"])
        digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
        self.value = {"schema_version": 1, "lock_id": config["build_lock_ref"], "deployment_sha256": digest(self.config),
                      "retention_policy": {"policy_id": policy["policy_id"], "policy_version": policy["policy_version"], "file_sha256": digest(self.retention)},
                      "releases": [{"provider": self.key[0], "implementation_version": self.key[1], "lifecycle_contract": manifest["contract_version"],
                                    "manifest_sha256": digest(self.manifest), "artifact_sha256": digest(self.artifact),
                                    "source": {"repository": "urn:design:unimplemented", "revision": "a" * 40}}]}
        self.write_lock()

    def write_lock(self):
        self.lock.write_text(json.dumps(self.value), encoding="utf-8")

    def inspect(self, artifacts=None):
        return inspect_lock(self.config, [self.manifest], self.lock, self.retention,
                            {self.key: self.artifact} if artifacts is None else artifacts)

    def test_matching_files_do_not_authorize_service(self):
        report = self.inspect()
        self.assertFalse(report["ready_for_service"])
        self.assertEqual(report["status"], "offline_locked_inputs_validated")
        self.assertIn("executed_artifact_identity", report["unverified"])
        self.assertIn("persistent_epochs", report["unverified"])

    def test_any_byte_change_to_locked_input_rejected(self):
        for path in (self.config, self.manifest, self.retention, self.artifact):
            with self.subTest(path=path.name):
                original = path.read_bytes()
                path.write_bytes(original + b"\n")
                with self.assertRaises(ValueError):
                    self.inspect()
                path.write_bytes(original)

    def test_release_coverage_is_exact_and_unique(self):
        baseline = copy.deepcopy(self.value)
        self.value["releases"].append(copy.deepcopy(self.value["releases"][0]))
        self.write_lock()
        with self.assertRaisesRegex(ValueError, "duplicate_release"):
            self.inspect()
        self.value = baseline
        self.value["releases"][0]["provider"] = "unselected-provider"
        self.write_lock()
        with self.assertRaisesRegex(ValueError, "coverage_mismatch"):
            self.inspect()

    def test_missing_or_extra_artifact_rejected(self):
        for mapping in ({}, {self.key: self.artifact, ("extra", "0.1.0"): self.artifact}):
            with self.assertRaisesRegex(ValueError, "coverage_mismatch"):
                self.inspect(mapping)

    def test_lifecycle_lock_and_retention_version_must_match(self):
        self.value["releases"][0]["lifecycle_contract"] = "2.0"
        self.write_lock()
        with self.assertRaisesRegex(ValueError, "manifest_mismatch"):
            self.inspect()
        self.value["releases"][0]["lifecycle_contract"] = "1.0"
        self.value["retention_policy"]["policy_version"] = 2
        self.write_lock()
        with self.assertRaisesRegex(ValueError, "retention_mismatch"):
            self.inspect()

    def test_lock_id_does_not_match_by_path_or_name(self):
        self.value["lock_id"] = "different-lock"
        self.write_lock()
        with self.assertRaisesRegex(ValueError, "deployment_mismatch"):
            self.inspect()

    def test_schema_rejects_mutable_revision_and_unknown_fields(self):
        from jsonschema import ValidationError
        baseline = copy.deepcopy(self.value)
        for mutation in (lambda x: x["releases"][0]["source"].update(revision="main"), lambda x: x.update(secret="sensitive"), lambda x: x["releases"][0].update(artifact_sha256="A"*64)):
            self.value = copy.deepcopy(baseline)
            mutation(self.value)
            self.write_lock()
            with self.assertRaises(ValidationError):
                self.inspect()

    def test_cli_failure_does_not_echo_input_paths_or_values(self):
        self.value["secret"] = "must-not-leak"
        self.write_lock()
        result = subprocess.run([sys.executable, str(ROOT/"check_build_lock.py"), "--config", str(self.config), "--manifest", str(self.manifest), "--lock", str(self.lock), "--retention", str(self.retention), "--artifact", *self.key, str(self.artifact)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stderr, "")
        self.assertNotIn("must-not-leak", result.stdout)
        self.assertNotIn(str(self.root), result.stdout)
        self.assertFalse(json.loads(result.stdout)["ready_for_service"])

    def test_changed_inputs_between_validation_and_lock_read_rejected(self):
        from unittest.mock import patch
        from check_config import inspect as inspect_config
        for target in (self.config, self.manifest):
            original = target.read_bytes()
            def changing_inspect(config, manifests):
                report = inspect_config(config, manifests)
                target.write_bytes(original + b"\n")
                return report
            with patch("check_build_lock.inspect", side_effect=changing_inspect):
                with self.assertRaisesRegex(ValueError, "input_changed_during_inspection"):
                    self.inspect()
            target.write_bytes(original)

    def test_lock_integer_fields_do_not_accept_integral_floats(self):
        baseline = copy.deepcopy(self.value)
        for mutation in (lambda x: x.update(schema_version=1.0), lambda x: x["retention_policy"].update(policy_version=1.0)):
            self.value = copy.deepcopy(baseline)
            mutation(self.value)
            self.write_lock()
            with self.assertRaisesRegex(ValueError, "integer_required"):
                self.inspect()


if __name__ == "__main__":
    unittest.main()

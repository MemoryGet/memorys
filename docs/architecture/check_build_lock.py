"""Verify local locked inputs offline. Never download, execute, or certify providers."""
import argparse
import hashlib
import json
from pathlib import Path
import sys

from jsonschema import Draft202012Validator

from check_config import ROOT, inspect, read_object
from retention_validation import validate_policy


def artifact_digest(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def inspect_lock(config_path, manifest_paths, lock_path, retention_path, artifacts):
    report = inspect(config_path, manifest_paths)
    config, config_hash = read_object(config_path)
    if config_hash != report["config_sha256"]:
        raise ValueError("lock_input_changed_during_inspection")
    lock, lock_hash = read_object(lock_path)
    policy, policy_hash = read_object(retention_path)
    schema = json.loads((ROOT / "schemas/build-lock.v1.schema.json").read_text(encoding="utf-8"))
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(lock)
    validate_policy(policy)
    if type(lock["schema_version"]) is not int or type(lock["retention_policy"]["policy_version"]) is not int:
        raise ValueError("lock_integer_required")
    if config["build_lock_ref"] != lock["lock_id"] or config_hash != lock["deployment_sha256"]:
        raise ValueError("lock_deployment_mismatch")
    if lock["retention_policy"] != {"policy_id": policy["policy_id"], "policy_version": policy["policy_version"], "file_sha256": policy_hash}:
        raise ValueError("lock_retention_mismatch")
    releases = {}
    for release in lock["releases"]:
        key = (release["provider"], release["implementation_version"])
        if key in releases:
            raise ValueError("lock_duplicate_release")
        releases[key] = release
    expected = {(module["provider"], module["implementation_version"]) for module in config["modules"]}
    manifests = {}
    checked_manifest_hashes = {(item["provider"], item["implementation_version"]): item["sha256"]
                              for item in report["manifests"]}
    for path in manifest_paths:
        manifest, digest = read_object(path)
        key = (manifest["module_id"], manifest["implementation_version"])
        if checked_manifest_hashes.get(key) != digest:
            raise ValueError("lock_input_changed_during_inspection")
        manifests[key] = (manifest, digest)
    if set(releases) != expected or set(manifests) != expected or set(artifacts) != expected:
        raise ValueError("lock_release_coverage_mismatch")
    for key, release in releases.items():
        manifest, digest = manifests[key]
        if release["manifest_sha256"] != digest or release["lifecycle_contract"] != manifest["contract_version"]:
            raise ValueError("lock_manifest_mismatch")
        if artifact_digest(artifacts[key]) != release["artifact_sha256"]:
            raise ValueError("lock_artifact_mismatch")
    report["status"] = "offline_locked_inputs_validated"
    report["build_lock_sha256"] = lock_hash
    report["retention_policy_sha256"] = policy_hash
    report["unverified"] = [item for item in report["unverified"] if item != "artifact_lock"]
    report["unverified"].extend(["trusted_lock_provenance", "source_build_attestation", "executed_artifact_identity", "backend_dependency_versions", "retention_holds"])
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, action="append", required=True)
    parser.add_argument("--lock", type=Path, required=True)
    parser.add_argument("--retention", type=Path, required=True)
    parser.add_argument("--artifact", nargs=3, action="append", required=True, metavar=("PROVIDER", "VERSION", "PATH"))
    args = parser.parse_args()
    try:
        artifacts = {}
        for provider, version, path in args.artifact:
            key = (provider, version)
            if key in artifacts:
                raise ValueError("duplicate_artifact_input")
            artifacts[key] = Path(path)
        print(json.dumps(inspect_lock(args.config, args.manifest, args.lock, args.retention, artifacts), ensure_ascii=False, indent=2))
        return 0
    except Exception:
        # No paths, settings, credentials, schema values or exception causes.
        print(json.dumps({"status": "rejected", "ready_for_service": False, "error": "locked_input_validation_failed"}))
        return 1


if __name__ == "__main__":
    sys.exit(main())

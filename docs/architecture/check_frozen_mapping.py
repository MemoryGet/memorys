"""Typed Go draft mapping checked against independent frozen Schema/Python golden identities."""
import json
import hashlib
from pathlib import Path
import subprocess
import tempfile

from check_architecture import source_inventory
from check_frozen_content import validate_payload
from canonical_content import frozen, canonical

ROOT = Path(__file__).resolve().parents[2]


def validate():
    before = source_inventory()
    parent = ROOT / ".local/architecture-check"
    parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="frozen-mapping-", dir=parent) as directory:
        executable = Path(directory) / "freeze.exe"
        subprocess.run([str(ROOT / ".tools/go/bin/go.exe"), "build", "-o", str(executable), "./cmd/architecture-freeze-draft"], cwd=ROOT, check=True, timeout=90)
        executable_sha256 = hashlib.sha256(executable.read_bytes()).hexdigest()
        samples = json.loads((ROOT / "docs/architecture/examples/frozen-content-golden.v1.json").read_text(encoding="utf-8"))["cases"]
        for sample in samples:
            result = subprocess.run([str(executable)], input=sample["input_json"], capture_output=True, text=True, encoding="utf-8", timeout=15)
            if result.returncode:
                raise RuntimeError(f"native freeze rejected {sample['name']}: {result.stderr[:300]}")
            mapped = json.loads(result.stdout)
            payload = mapped["payload"].encode("utf-8")
            validate_payload(json.loads(payload))
            if mapped["payload"] != sample["canonical_json"] or mapped["hashes"] != sample["hashes"] or frozen(payload) != mapped["hashes"] or canonical(payload) != payload:
                raise RuntimeError(f"native freeze differs from independent reference: {sample['name']}")
    if before != source_inventory():
        raise RuntimeError("sources changed during freeze mapping checks")
    return {"passed": True, "production_acceptance": False, "scope": "Typed native draft mapping to independent frozen Schema/Python golden bytes and hashes", "cases": [s["name"] for s in samples], "sources": before, "sources_unchanged_during_checks": True, "executable_sha256": executable_sha256, "temporary_resources_removed": not Path(directory).exists(), "unverified": ["original/media authenticity and resource authorization", "authoritative submit/revision/approval/outbox transaction", "concurrent submit/restart/replay", "production deployment"]}


def main():
    report_path = ROOT / ".local/architecture-check/frozen-mapping.json"
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report = {"passed": False, "production_acceptance": False, "status": "running"}
    report_path.write_text(json.dumps(report) + "\n", encoding="utf-8")
    try:
        report = validate()
    except Exception as error:
        report.update(status="failed", error=type(error).__name__)
        report_path.write_text(json.dumps(report) + "\n", encoding="utf-8")
        raise
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Native frozen draft mapping: {len(report['cases'])} independent Schema/Python golden cases passed")


if __name__ == "__main__":
    main()

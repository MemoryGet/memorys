"""Repeatable local architecture checks; never a production acceptance gate."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[2]


def source_inventory(root=ROOT):
    paths = [root / "go.mod", root / "go.sum",
             root / "docs/architecture/evidence/openapi-validation-requirements.txt"]
    for tree in ("pkg", "cmd", "tests", "docs/architecture", "tools/parsers"):
        for p in (root / tree).rglob("*"):
            parts = p.relative_to(root).parts
            if not p.is_file() or "__pycache__" in parts or "evidence" in parts:
                continue
            # Future binary/parser samples and Go testdata are inputs too;
            # do not limit fixture hashes to JSON or text extensions.
            if p.suffix in {".go", ".py", ".json", ".txt", ".yaml", ".yml", ".sql"} or "testdata" in parts or "examples" in parts:
                paths.append(p)
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(set(paths)) if p.is_file()}


def execute(command, timeout):
    start = time.monotonic()
    try:
        result = subprocess.run(command, cwd=ROOT, capture_output=True,
                                text=True, encoding="utf-8", errors="replace", timeout=timeout)
        return {"command": command, "exit_code": result.returncode,
                "status": "passed" if result.returncode == 0 else "failed",
                "stdout": result.stdout, "stderr": result.stderr,
                "elapsed_seconds": round(time.monotonic() - start, 3)}
    except (OSError, subprocess.TimeoutExpired) as e:
        return {"command": command, "exit_code": None, "status": "failed",
                "error": type(e).__name__, "detail": str(e),
                "elapsed_seconds": round(time.monotonic() - start, 3)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, default=ROOT / ".local/architecture-check/latest.json")
    parser.add_argument("--timeout", type=int, default=300,
                        help="Seconds per command; a timeout fails that check")
    args = parser.parse_args()
    if args.timeout < 1:
        parser.error("timeout must be positive")
    go = str(ROOT / ".tools/go/bin/go.exe")
    spec_python = str(ROOT / ".local/openapi-review-env/Scripts/python.exe")
    plan = [
        ("go_tests_including_import_boundaries", [go, "test", "-count=1", "./..."]),
        ("go_vet", [go, "vet", "./..."]),
        ("python_architecture_contract_tests", [sys.executable, "-m", "unittest", "discover",
                                              "-s", "docs/architecture", "-p", "test_*.py"]),
        ("all_openapi_structure", [spec_python, "docs/architecture/check_openapi_conformance.py"]),
    ]
    before = source_inventory()
    checks = []
    for name, command in plan:
        print(f"Starting {name}", flush=True)
        result = execute(command, args.timeout)
        result["id"] = name
        checks.append(result)
        print(f"{name}: {result['status']}", flush=True)
    after = source_inventory()
    unchanged = before == after
    passed = unchanged and all(c["status"] == "passed" for c in checks)
    report = {
        "recorded_at": datetime.now(timezone.utc).isoformat(),
        "scope": "Windows local architecture regression: Go, direct imports, Python contracts, all three OpenAPI specifications",
        "local_checks_passed": passed,
        "production_acceptance": False,
        "sources_unchanged_during_checks": unchanged,
        "sources": before,
        "input_inventory_scope": "Go/Python/schema/config sources; all examples/testdata bytes; reviewed OpenAPI dependency lock. Generated evidence and caches excluded.",
        "environment_provenance_limit": "Executable paths are recorded; binary identity and installed transitive package contents are not verified.",
        "checks": checks,
        "unverified": ["API handler behavior, semantic compatibility and MCP interoperability",
                       "real authentication, transactions and adapters",
                       "replacement, migration and rollback",
                       "sealed Chinese Recall@10, performance and actual cost"],
    }
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
    print(f"Report: {args.report.resolve()}", flush=True)
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())

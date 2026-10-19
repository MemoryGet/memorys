"""Run the machine-readable local acceptance plan and emit Codex evidence."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = Path(__file__).with_name("ai_acceptance_manifest.json")


def resolve_token(token):
    if token == "go":
        return str(ROOT / ".tools/go/bin/go.exe")
    if token == "spec_python":
        return str(ROOT / ".local/openapi-review-env/Scripts/python.exe")
    if token == "python":
        return sys.executable
    return token


def run_case(case, timeout):
    command = [resolve_token(t) for t in case["command"]]
    started = time.monotonic()
    try:
        p = subprocess.run(command, cwd=ROOT, capture_output=True, text=True,
                           encoding="utf-8", errors="replace", timeout=timeout)
        status = "passed" if p.returncode == 0 else "failed"
        return {"id": case["id"], "status": status, "exit_code": p.returncode,
                "command": command, "stdout": p.stdout[-12000:], "stderr": p.stderr[-12000:],
                "elapsed_seconds": round(time.monotonic() - started, 3)}
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"id": case["id"], "status": "inconclusive", "exit_code": None,
                "command": command, "error": type(exc).__name__, "detail": str(exc),
                "elapsed_seconds": round(time.monotonic() - started, 3)}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--tier", choices=("static", "runtime", "all"), default="static")
    ap.add_argument("--only", action="append", help="suite or case id; repeatable")
    ap.add_argument("--timeout", type=int, default=300)
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--report", type=Path,
                    default=ROOT / "docs/architecture/evidence/ai-acceptance-latest.json")
    args = ap.parse_args()
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    wanted = set(args.only or [])
    results = []
    for suite in manifest["suites"]:
        if args.tier != "all" and suite["tier"] != args.tier:
            continue
        if wanted and suite["id"] not in wanted and not any(c["id"] in wanted for c in suite["cases"]):
            continue
        for case in suite["cases"]:
            if wanted and suite["id"] not in wanted and case["id"] not in wanted:
                continue
            results.append({"suite": suite["id"], "tier": suite["tier"],
                            **({"status": "planned", "id": case["id"], "command": [resolve_token(t) for t in case["command"]]}
                               if args.dry_run else run_case(case, args.timeout))})
    required = [r for r in results if any(s["id"] == r["suite"] and s["required"] for s in manifest["suites"])]
    passed = bool(results) and all(r["status"] == "passed" for r in required)
    report = {
        "schema_version": "ai-acceptance-result.v1",
        "recorded_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "root": str(ROOT),
        "tier": args.tier,
        "local_functional_acceptance": passed and not args.dry_run,
        "production_acceptance": False,
        "fail_closed": manifest["policy"]["fail_closed"],
        "results": results,
        "ai_decision": "pass" if passed and not args.dry_run else ("planned" if args.dry_run else "fail"),
        "unverified": ["real production dependencies", "production IAM and deployment provenance",
                       "Recall@10 and production performance", "backup/restore and long-duration soak"],
    }
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"report": str(args.report.resolve()), "ai_decision": report["ai_decision"],
                      "local_functional_acceptance": report["local_functional_acceptance"]}, ensure_ascii=False))
    return 0 if report["ai_decision"] in ("pass", "planned") else 1


if __name__ == "__main__":
    raise SystemExit(main())

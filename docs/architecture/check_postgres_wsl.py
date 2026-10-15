"""Owned temporary PostgreSQL on WSL; real-engine evidence, not production acceptance."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import uuid

from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]


def run(command, timeout=90, env=None, check=True):
    result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True,
                            encoding="utf-8", errors="replace", timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(f"{command[:3]} failed: {result.stderr[-1600:]}")
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--distribution", default="Ubuntu")
    parser.add_argument("--port", type=int, default=55432)
    parser.add_argument("--major", type=int, choices=(15, 16, 18), default=16)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    if args.report is None:
        args.report = ROOT / f".local/architecture-check/postgres{args.major}-wsl.json"
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,60}", args.distribution) or not 1024 <= args.port <= 65535:
        parser.error("invalid local fixture configuration")
    prefix = ["wsl", "--distribution", args.distribution, "--exec"]
    linux_root = run(prefix + ["wslpath", "-a", str(ROOT)]).stdout.strip()
    if not linux_root.startswith("/mnt/") or "\n" in linux_root:
        raise RuntimeError("unexpected workspace mapping")
    bundle = linux_root + f"/.local/postgres{args.major}-ubuntu/root"
    binaries = bundle + f"/usr/lib/postgresql/{args.major}/bin/"
    wsl_env = ["env", "LD_LIBRARY_PATH=" + bundle + "/usr/lib/x86_64-linux-gnu"]

    def wsl(*command, check=True, timeout=90):
        return run(prefix + wsl_env + list(command), check=check, timeout=timeout)

    report = {"passed": False, "production_acceptance": False,
              "scope": f"WSL PostgreSQL {args.major} real SQL/repository probe; not production or exact Docker image acceptance",
              "requested_major": args.major,
              "distribution": args.distribution, "port": args.port,
              "server_stopped": False, "temporary_resources_removed": False}
    before = source_inventory()
    folder = None
    marker = uuid.uuid4().hex
    started_or_attempted = False
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report) + "\n", encoding="utf-8")
    try:
        if wsl("uname", "-m").stdout.strip() != "x86_64":
            raise RuntimeError("fixture expects x86_64 WSL")
        report["binary_version"] = wsl(binaries + "postgres", "--version").stdout.strip()
        report["postgres_binary_sha256"] = hashlib.sha256((ROOT / f".local/postgres{args.major}-ubuntu/root/usr/lib/postgresql/{args.major}/bin/postgres").read_bytes()).hexdigest()
        if wsl("ss", "-ltnH", "sport = :" + str(args.port)).stdout.strip():
            raise RuntimeError("fixture port is already occupied; existing process untouched")
        folder = wsl("mktemp", "-d", "/tmp/akos-pg-XXXXXXXXXX").stdout.strip()
        if not re.fullmatch(r"/tmp/akos-pg-[A-Za-z0-9]{10}", folder):
            raise RuntimeError("invalid owned temporary directory")
        report["temporary_directory"] = folder
        marker_path = folder + "/.akos-owned-" + marker
        wsl("touch", marker_path)
        data = folder + "/data"
        wsl(binaries + "initdb", "-D", data, "-L", bundle + f"/usr/share/postgresql/{args.major}", "-U", "postgres", "-A", "trust", "--no-locale", "--encoding=UTF8")
        started_or_attempted = True
        options = f"-h 127.0.0.1 -p {args.port} -c unix_socket_directories={folder} -c shared_buffers=32MB -c max_connections=20 -c jit=off"
        wsl(binaries + "pg_ctl", "-D", data, "-l", folder + "/server.log", "-w", "-t", "20", "-o", options, "start", timeout=40)
        psql = [binaries + "psql", "-X", "-h", "127.0.0.1", "-p", str(args.port), "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1"]
        report["server_version"] = wsl(*psql, "-At", "-c", "SHOW server_version").stdout.strip()
        if not report["server_version"].startswith(str(args.major) + "."):
            raise RuntimeError("server major differs from requested fixture")
        report["sql_checks"] = []
        for name in ("withdraw_contract_v1.sql", "withdraw_contract_test.sql"):
            output = wsl(*psql, "-f", linux_root + "/docs/architecture/sql/" + name).stdout
            if name.endswith("_test.sql") and "PASS: PostgreSQL withdrawal constraint prototype" not in output:
                raise RuntimeError("SQL completion marker absent")
            report["sql_checks"].append({"file": name, "completed": True})
        with tempfile.TemporaryDirectory(prefix="pg-wsl-", dir=ROOT / ".local/architecture-check") as directory:
            executable = Path(directory) / "probe"
            environment = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
            run([str(ROOT / ".tools/go/bin/go.exe"), "build", "-o", str(executable), "./cmd/architecture-pg-repositories"], env=environment, timeout=120)
            report["go_executable_sha256"] = hashlib.sha256(executable.read_bytes()).hexdigest()
            linux_executable = run(prefix + ["wslpath", "-a", str(executable)]).stdout.strip()
            proof = json.loads(wsl("env", "AKOS_TEST_PG_PORT=" + str(args.port), linux_executable, timeout=60).stdout)
            if proof.get("passed") is not True or proof.get("production_acceptance") is not False:
                raise RuntimeError("repository probe rejected")
            report["go_repository_proof"] = proof
        if before != source_inventory():
            raise RuntimeError("sources changed during verification")
        report["sources"] = before
        report["sources_unchanged_during_checks"] = True
        report["passed"] = True
    except Exception as error:
        report["error"] = str(error)
        if folder and re.fullmatch(r"/tmp/akos-pg-[A-Za-z0-9]{10}", folder):
            # Fixture diagnostics only: preserve ERROR summaries, never SQL
            # statements/parameters or full content-bearing DETAIL lines.
            try:
                wsl("test", "-f", folder + "/.akos-owned-" + marker)
                errors = "import pathlib,sys,json; p=pathlib.Path(sys.argv[1]); lines=p.read_text(errors='replace').splitlines() if p.is_file() else []; print(json.dumps([line.split('ERROR:',1)[1].strip()[:500] for line in lines if 'ERROR:' in line][-10:]))"
                report["fixture_server_error_summaries"] = json.loads(wsl("python3", "-c", errors, folder + "/server.log").stdout)
            except Exception:
                report["fixture_server_diagnostics_unavailable"] = True
    finally:
        if folder and re.fullmatch(r"/tmp/akos-pg-[A-Za-z0-9]{10}", folder):
            try:
                wsl("test", "-f", folder + "/.akos-owned-" + marker)
                if started_or_attempted:
                    stopped = wsl(binaries + "pg_ctl", "-D", folder + "/data", "-m", "fast", "-w", "-t", "20", "stop", check=False, timeout=40)
                    if stopped.returncode and wsl(binaries + "pg_ctl", "-D", folder + "/data", "status", check=False).returncode != 3:
                        raise RuntimeError("owned server is still live; directory retained")
                report["server_stopped"] = True
                cleanup = "import os,pathlib,shutil,sys; p=pathlib.Path(sys.argv[1]); assert p.parent==pathlib.Path('/tmp') and p.name.startswith('akos-pg-') and not p.is_symlink() and p.stat().st_uid==os.getuid() and (p/('.akos-owned-'+sys.argv[2])).is_file(); shutil.rmtree(p)"
                wsl("python3", "-c", cleanup, folder, marker)
                report["temporary_resources_removed"] = True
            except Exception as error:
                report["cleanup_error"] = str(error)
                report["passed"] = False
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("WSL PostgreSQL real-engine probe: " + ("passed" if report["passed"] else "failed"))
    if not report["passed"]:
        print(report.get("error", report.get("cleanup_error", "unverified")))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())

"""Disposable real-PostgreSQL constraint verification; not an application gate."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]


def run(args, timeout=60, env=None):
    r = subprocess.run(args, cwd=ROOT, capture_output=True, text=True,
                       encoding="utf-8", errors="replace", timeout=timeout, env=env)
    if r.returncode:
        raise RuntimeError(f"{args[0:3]} exit {r.returncode}: {r.stderr[-2000:]}")
    return r.stdout


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--image", default="postgres:18.0-alpine")
    p.add_argument("--go-repositories", action="store_true")
    p.add_argument("--report", type=Path,
                   default=ROOT / ".local/architecture-check/postgres-withdraw.json")
    args = p.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.:/@-]*", args.image):
        p.error("invalid image")
    sql = [ROOT / "docs/architecture/sql/withdraw_contract_v1.sql",
           ROOT / "docs/architecture/sql/withdraw_contract_test.sql"]
    hashes = {str(f.relative_to(ROOT)): hashlib.sha256(f.read_bytes()).hexdigest()
              for f in sql}
    report = {"scope": "PostgreSQL SQL constraint prototype only",
              "passed": False, "production_acceptance": False, "sources": hashes,
              "image": args.image, "container_removed": False}
    container = None
    marker = uuid.uuid4().hex
    name = "akos-withdraw-" + marker[:12]
    executable = None
    try:
        endpoint = os.environ.get("DOCKER_HOST") or run([
            "docker", "context", "inspect", "--format",
            "{{.Endpoints.docker.Host}}"]).strip()
        if not endpoint.startswith(("npipe://", "unix://")):
            raise RuntimeError("requires local Docker socket")
        image = json.loads(run(["docker", "image", "inspect", args.image]))[0]
        report["image_identity"] = image["Id"]
        if args.go_repositories:
            if image["Os"] != "linux" or image["Architecture"] not in {"amd64", "arm64"}:
                raise RuntimeError("unsupported probe target platform")
            executable = ROOT / ".local/architecture-check" / ("pg-repositories-" + marker)
            executable.parent.mkdir(parents=True, exist_ok=True)
            env = os.environ.copy()
            env.update(GOOS="linux", GOARCH=image["Architecture"], CGO_ENABLED="0")
            run([str(ROOT / ".tools/go/bin/go.exe"), "build", "-o", str(executable),
                 "./cmd/architecture-pg-repositories"], timeout=120, env=env)
            report["go_executable_sha256"] = hashlib.sha256(executable.read_bytes()).hexdigest()
        # Preserve the owned name before docker run: creation can succeed even
        # when startup fails and the CLI returns nonzero without an ID.
        container = name
        created = run(["docker", "run", "--detach",
                         "--name", name,
                         "--label", "ai.memorys.test=" + marker,
                         "--network", "none", "--memory", "512m", "--cpus", "1",
                         "--tmpfs", "/var/lib/postgresql/data:rw,size=256m",
                         "--env", "PGDATA=/var/lib/postgresql/data",
                         "--env", "POSTGRES_HOST_AUTH_METHOD=trust",
                         args.image]).strip()
        if not re.fullmatch(r"[0-9a-f]{64}", created):
            raise RuntimeError("invalid created container identity")
        container = created
        deadline = time.monotonic() + 30
        while True:
            ready = subprocess.run(["docker", "exec", container, "pg_isready",
                                    "-U", "postgres"], capture_output=True, timeout=10)
            if ready.returncode == 0:
                break
            if time.monotonic() >= deadline:
                raise RuntimeError("PostgreSQL did not become ready")
            time.sleep(0.5)
        report["server_version"] = run([
            "docker", "exec", "--user", "postgres", container, "psql",
            "-U", "postgres", "-d", "postgres", "-At",
            "-c", "SHOW server_version;"]).strip()
        outputs = []
        for f in sql:
            target = "/tmp/" + f.name
            run(["docker", "cp", str(f), container + ":" + target])
            outputs.append(run(["docker", "exec", "--user", "postgres", container,
                                "psql", "-U", "postgres", "-d", "postgres",
                                "-v", "ON_ERROR_STOP=1", "-f", target]))
        report["outputs"] = outputs
        if "PASS: PostgreSQL withdrawal constraint prototype" not in outputs[-1]:
            raise RuntimeError("missing SQL completion marker")
        if executable:
            run(["docker", "cp", str(executable), container + ":/tmp/pg-repositories"])
            run(["docker", "exec", container, "chmod", "755", "/tmp/pg-repositories"])
            proof = json.loads(run(["docker", "exec", "--user", "postgres", container,
                                    "/tmp/pg-repositories"]))
            if proof.get("passed") is not True or proof.get("production_acceptance") is not False:
                raise RuntimeError("invalid Go repository proof")
            report["go_repository_proof"] = proof
        for f in sql:
            if hashlib.sha256(f.read_bytes()).hexdigest() != hashes[str(f.relative_to(ROOT))]:
                raise RuntimeError("SQL inputs changed during verification")
        report["passed"] = True
    except Exception as e:
        report["error"] = str(e)
    finally:
        if executable:
            executable.unlink(missing_ok=True)
        if container:
            try:
                labels = json.loads(run(["docker", "inspect", container]))[0]["Config"]["Labels"]
                if labels.get("ai.memorys.test") != marker:
                    raise RuntimeError("container ownership mismatch")
                run(["docker", "rm", "--force", "--volumes", container])
                report["container_removed"] = True
            except Exception as e:
                report["cleanup_error"] = str(e)
                report["passed"] = False
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n",
                               encoding="utf-8")
    print(f"PostgreSQL constraint prototype: {'passed' if report['passed'] else 'failed'}")
    print(f"Report: {args.report}")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())

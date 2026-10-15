"""Same PDF, two independent extraction engines; not production acceptance."""
import base64
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

from check_architecture import source_inventory
from check_markitdown_runtime import fixtures

ROOT = Path(__file__).resolve().parents[2]


def negative_pdfs():
    # Generate real PDF containers with the independent backend's writer.
    code = """import base64,io,json
from pypdf import PdfWriter
result={}
for name in ('blank','encrypted'):
    writer=PdfWriter()
    writer.add_blank_page(width=612,height=792)
    if name=='encrypted': writer.encrypt('fixture-password')
    stream=io.BytesIO()
    writer.write(stream)
    result[name]=base64.b64encode(stream.getvalue()).decode()
print(json.dumps(result))
"""
    python = ROOT / ".local/pypdf-env/Scripts/python.exe"
    result = subprocess.run([str(python), "-I", "-c", code], check=True, capture_output=True, text=True, encoding="utf-8", timeout=45)
    values = {name: base64.b64decode(value, validate=True) for name, value in json.loads(result.stdout).items()}
    values["truncated"] = b"%PDF-1.4\ntruncated"
    return values


def main():
    parent = ROOT / ".local/architecture-check"
    parent.mkdir(parents=True, exist_ok=True)
    report_path = parent / "parser-replacement.json"
    report_path.write_text('{"passed":false,"status":"running"}\n', encoding="utf-8")
    before = source_inventory()
    records = []
    negative_records = []
    with tempfile.TemporaryDirectory(prefix="parser-replace-", dir=parent) as directory:
        folder = Path(directory)
        executable = folder / "probe.exe"
        subprocess.run([str(ROOT / ".tools/go/bin/go.exe"), "build", "-o", str(executable), "./cmd/architecture-office-parser"], cwd=ROOT, check=True, timeout=90)
        executable_sha = hashlib.sha256(executable.read_bytes()).hexdigest()
        template, _ = fixtures()["pdf"]
        for word in ("Hello", "Basic", "Other"):
            expected = word + " PDF knowledge"
            raw = template.replace(b"Hello PDF knowledge", expected.encode())
            source = folder / "source.pdf"
            source.write_bytes(raw)
            source_sha = hashlib.sha256(raw).hexdigest()
            for provider, environment, worker, version in (
                    ("platform.markitdown-office", "markitdown-env", "markitdown_worker.py", "markitdown/0.1.3"),
                    ("platform.pypdf", "pypdf-env", "pypdf_worker.py", "pypdf/6.19.0")):
                python = ROOT / ".local" / environment / "Scripts/python.exe"
                script = ROOT / "tools/parsers" / worker
                command = [str(executable), "--provider", provider, "--python", str(python), "--worker", str(script), "--source", str(source), "--format", "pdf", "--root", str(folder / word / provider)]
                output = subprocess.run(command, capture_output=True, text=True, encoding="utf-8", timeout=45)
                if output.returncode:
                    raise RuntimeError(f"{word}/{provider}: {output.stderr[:500]}")
                result = json.loads(output.stdout)
                assert result["passed"] is True and result["production_acceptance"] is False
                assert result["configured_factory"] is True and result["closed_reference_rejected"] is True
                assert result["candidate_merge"] is True and result["late_candidate_preserved"] is True
                assert result["provider"] == provider and result["parser_version"] == version
                assert result["source_sha256"] == source_sha and result["source_precision"] == "coarse"
                assert " ".join(result["text"].split()) == expected
                # Both implementations must reject a corrupt source via the actual worker.
                request = {"format": "pdf", "sha256": "0" * 64, "data_base64": base64.b64encode(raw).decode()}
                rejected = subprocess.run([str(python), "-I", str(script)], input=json.dumps(request), capture_output=True, text=True, encoding="utf-8", timeout=45)
                assert rejected.returncode != 0
                records.append({"provider": provider, "parser_version": version, "source_sha256": source_sha, "expected_text_sha256": hashlib.sha256(expected.encode()).hexdigest(), "same_text": True, "source_preserved": True, "configured_factory": True, "closed_reference_rejected": True, "corrupt_digest_rejected": True, "candidate_merge": True, "late_candidate_preserved": True})
        for name, raw in negative_pdfs().items():
            for provider, environment, worker in (
                    ("platform.markitdown-office", "markitdown-env", "markitdown_worker.py"),
                    ("platform.pypdf", "pypdf-env", "pypdf_worker.py")):
                request = {"format": "pdf", "sha256": hashlib.sha256(raw).hexdigest(), "data_base64": base64.b64encode(raw).decode()}
                result = subprocess.run([str(ROOT / ".local" / environment / "Scripts/python.exe"), "-I", str(ROOT / "tools/parsers" / worker)], input=json.dumps(request), capture_output=True, text=True, encoding="utf-8", timeout=45)
                if result.returncode == 0 or result.stdout:
                    raise RuntimeError(f"{provider}: unsupported {name} PDF produced a candidate")
                negative_records.append({"provider": provider, "case": name, "source_sha256": request["sha256"], "rejected_without_candidate": True})
    assert before == source_inventory(), "sources changed during validation"
    report = {"passed": True, "production_acceptance": False, "scope": "Three ASCII born-digital PDFs, two independently installed extraction backends, same standard Parser caller", "records": records, "sources": before, "sources_unchanged_during_checks": True, "probe_executable_sha256": executable_sha, "temporary_resources_removed": not folder.exists(), "unverified": ["Chinese/complex/scanned PDF and structured tables", "full UI/API/MCP and current authorization", "deployment loader, candidate review/publish and migration rollback", "OS process-tree and memory isolation", "quality/latency/cost on a sealed evaluation corpus"]}
    report["negative_records"] = negative_records
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Parser replacement: three PDFs x two real backends passed")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        report_path = ROOT / ".local/architecture-check/parser-replacement.json"
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps({"passed": False, "production_acceptance": False, "status": "failed", "error": type(error).__name__}) + "\n", encoding="utf-8")
        raise

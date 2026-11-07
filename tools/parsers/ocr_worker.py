"""Bounded local OCR worker; communicates only through stdin/stdout JSON."""
import base64
import hashlib
import io
import json
import math
import os
import shutil
import subprocess
import sys
import tempfile


VERSION = "ocr/tesseract-5"


def run(command, payload=None, timeout=90):
    return subprocess.run(command, input=payload, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=True)


def health():
    if shutil.which("tesseract") is None:
        raise ValueError("tesseract unavailable")
    run(["tesseract", "--version"], timeout=10)
    sys.stdout.write(json.dumps({"version": VERSION, "healthy": True}))


def ocr_image(raw, language):
    result = run(["tesseract", "stdin", "stdout", "--dpi", "300", "-l", language, "--psm", "3"], raw)
    text = result.stdout.decode("utf-8", errors="strict").strip()
    if not text:
        raise ValueError("empty OCR result")
    tsv = run(["tesseract", "stdin", "stdout", "--dpi", "300", "-l", language, "--psm", "3", "tsv"], raw)
    confidences = []
    for line in tsv.stdout.decode("utf-8", errors="strict").splitlines()[1:]:
        columns = line.split("\t")
        if len(columns) < 11 or not columns[11].strip():
            continue
        try:
            value = float(columns[10])
        except ValueError:
            continue
        if math.isfinite(value) and 0 <= value <= 100:
            confidences.append(value)
    confidence = sum(confidences) / len(confidences) if confidences else 0.0
    return text, round(confidence, 3)


def ocr_pdf(raw, language):
    if shutil.which("pdftoppm") is None:
        raise ValueError("PDF OCR helper unavailable")
    with tempfile.TemporaryDirectory(prefix="memorys-ocr-") as directory:
        source = os.path.join(directory, "source.pdf")
        prefix = os.path.join(directory, "page")
        with open(source, "wb") as handle:
            handle.write(raw)
        run(["pdftoppm", "-png", "-r", "200", "-f", "1", "-l", "200", source, prefix], timeout=120)
        pages, confidences = [], []
        for name in sorted(os.listdir(directory)):
            if not name.startswith("page-") or not name.endswith(".png"):
                continue
            with open(os.path.join(directory, name), "rb") as handle:
                text, confidence = ocr_image(handle.read(), language)
                pages.append(text)
                confidences.append(confidence)
        if not pages:
            raise ValueError("empty PDF OCR result")
        return pages, confidences


def main():
    if sys.argv[1:] == ["--health"]:
        health()
        return
    request = json.loads(sys.stdin.buffer.read((12 << 20) + 1))
    if set(request) != {"format", "sha256", "data_base64"} or request["format"] not in {"png", "jpg", "jpeg", "pdf"}:
        raise ValueError("unsupported request")
    raw = base64.b64decode(request["data_base64"], validate=True)
    if not raw or len(raw) > 8 << 20 or hashlib.sha256(raw).hexdigest() != request["sha256"]:
        raise ValueError("invalid source")
    language = os.environ.get("MEMORYS_OCR_LANG", "eng")
    if len(language) > 64 or any(ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+-" for ch in language):
        raise ValueError("invalid OCR language")
    if request["format"] == "pdf":
        pages, confidences = ocr_pdf(raw, language)
    else:
        text, confidence = ocr_image(raw, language)
        pages, confidences = [text], [confidence]
    text = "\n\n".join(pages)
    if len(text.encode("utf-8")) > 4 << 20:
        raise ValueError("OCR result too large")
    sys.stdout.buffer.write(json.dumps({"version": VERSION, "markdown": text, "pages": pages, "ocr_confidence": confidences}, ensure_ascii=False).encode("utf-8"))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("OCR rejected\n")
        sys.exit(1)

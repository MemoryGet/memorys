"""Bounded local audio transcription worker using an installed whisper CLI."""
import base64
import hashlib
import json
import math
import os
import shutil
import subprocess
import sys
import tempfile


VERSION = "transcription/whisper-1"


def run(command, timeout=180):
    return subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=True)


def health():
    if shutil.which("whisper") is None:
        raise ValueError("whisper unavailable")
    run(["whisper", "--help"], timeout=15)
    sys.stdout.write(json.dumps({"version": VERSION, "healthy": True}))


def main():
    if sys.argv[1:] == ["--health"]:
        health()
        return
    request = json.loads(sys.stdin.buffer.read((12 << 20) + 1))
    if set(request) != {"format", "sha256", "data_base64"} or request["format"] not in {"wav", "mp3", "m4a"}:
        raise ValueError("unsupported request")
    raw = base64.b64decode(request["data_base64"], validate=True)
    if not raw or len(raw) > 8 << 20 or hashlib.sha256(raw).hexdigest() != request["sha256"]:
        raise ValueError("invalid source")
    model = os.environ.get("MEMORYS_TRANSCRIPTION_MODEL", "base")
    if len(model) > 64 or any(ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-" for ch in model):
        raise ValueError("invalid transcription model")
    language = os.environ.get("MEMORYS_TRANSCRIPTION_LANG", "")
    with tempfile.TemporaryDirectory(prefix="memorys-transcription-") as directory:
        source = os.path.join(directory, "source." + request["format"])
        with open(source, "wb") as handle:
            handle.write(raw)
        command = ["whisper", source, "--model", model, "--output_format", "json", "--output_dir", directory, "--fp16", "False"]
        if language:
            command.extend(["--language", language])
        run(command)
        output = os.path.join(directory, "source.json")
        with open(output, "r", encoding="utf-8") as handle:
            result = json.load(handle)
    raw_segments = result.get("segments")
    if not isinstance(raw_segments, list) or len(raw_segments) > 10000:
        raise ValueError("missing or oversized segments")
    segments = []
    for item in raw_segments:
        if not isinstance(item, dict) or set(item) - {"id", "seek", "start", "end", "text", "tokens", "temperature", "avg_logprob", "compression_ratio", "no_speech_prob"}:
            raise ValueError("invalid segment")
        start, end, value = item.get("start"), item.get("end"), item.get("text")
        if not isinstance(start, (int, float)) or not isinstance(end, (int, float)) or not math.isfinite(start) or not math.isfinite(end) or start < 0 or end < start or end > 24 * 60 * 60 or not isinstance(value, str):
            raise ValueError("invalid segment range")
        value = value.strip()
        if not value:
            continue
        segments.append({"start_ms": int(round(start * 1000)), "end_ms": int(round(end * 1000)), "text": value})
    text = "\n\n".join(item["text"] for item in segments).strip()
    if not text or len(text.encode("utf-8")) > 4 << 20:
        raise ValueError("empty or oversized transcription")
    if not segments:
        raise ValueError("empty transcription segments")
    sys.stdout.buffer.write(json.dumps({"version": VERSION, "markdown": text, "segments": segments}, ensure_ascii=False).encode("utf-8"))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("transcription rejected\n")
        sys.exit(1)

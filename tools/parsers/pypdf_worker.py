"""Stream-only pypdf 6.19.0 text projection. No OCR, paths or publishing."""
import base64
import hashlib
import importlib.metadata
import io
import json
import socket
import sys


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate field")
        result[key] = value
    return result


def denied(*args, **kwargs):
    raise PermissionError("network disabled")


def main():
    if importlib.metadata.version("pypdf") != "6.19.0":
        raise ValueError("wrong converter version")
    from pypdf import PdfReader
    if sys.argv[1:] == ["--health"]:
        sys.stdout.buffer.write(b'{"version":"pypdf/6.19.0","healthy":true}')
        return
    if sys.argv[1:]:
        raise ValueError("unexpected arguments")
    encoded = sys.stdin.buffer.read((12 << 20) + 1)
    if len(encoded) > 12 << 20:
        raise ValueError("request budget exceeded")
    request = json.loads(encoded, object_pairs_hook=pairs)
    if (not isinstance(request, dict) or set(request) != {"format", "sha256", "data_base64"}
            or request["format"] != "pdf"):
        raise ValueError("unsupported request")
    raw = base64.b64decode(request["data_base64"], validate=True)
    if (not raw.startswith(b"%PDF-") or len(raw) > 8 << 20
            or hashlib.sha256(raw).hexdigest() != request["sha256"]):
        raise ValueError("invalid source")
    socket.create_connection = denied
    socket.socket.connect = denied
    socket.socket.connect_ex = denied
    reader = PdfReader(io.BytesIO(raw), strict=True)
    if reader.is_encrypted or not 0 < len(reader.pages) <= 200:
        raise ValueError("unsupported PDF")
    parts, output_size, stream_size = [], 0, 0
    for page in reader.pages:
        content = page.get_contents()
        if content is not None:
            stream_size += len(content.get_data())
        if stream_size > 64 << 20:
            raise ValueError("content budget exceeded")
        text = page.extract_text() or ""
        output_size += len(text.encode("utf-8")) + 2
        if output_size > 4 << 20:
            raise ValueError("output budget exceeded")
        parts.append(text)
    text = "\n\n".join(parts)
    if not text.strip():
        raise ValueError("no text layer")
    sys.stdout.buffer.write(json.dumps({"version": "pypdf/6.19.0", "markdown": text}, ensure_ascii=False).encode("utf-8"))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("document conversion rejected\n")
        sys.exit(1)

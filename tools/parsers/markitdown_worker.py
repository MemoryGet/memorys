"""Single-file, stream-only MarkItDown 0.1.3 worker; no publishing or file paths."""
import base64
import hashlib
import importlib.metadata
import io
import json
import socket
import sys
import zipfile


def denied(*args, **kwargs):
    raise PermissionError("network disabled")


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate field")
        result[key] = value
    return result


def main():
    if sys.argv[1:] == ["--health"]:
        if importlib.metadata.version("markitdown") != "0.1.3":
            raise ValueError("wrong converter version")
        from markitdown.converters import DocxConverter, XlsxConverter, PptxConverter, PdfConverter
        sys.stdout.buffer.write(b'{"version":"markitdown/0.1.3","healthy":true}')
        return
    request = json.loads(sys.stdin.buffer.read((12 << 20) + 1), object_pairs_hook=pairs)
    if set(request) != {"format", "sha256", "data_base64"} or request["format"] not in {"docx", "xlsx", "pptx", "pdf"}:
        raise ValueError("unsupported request")
    raw = base64.b64decode(request["data_base64"], validate=True)
    if not raw or len(raw) > 8 << 20 or hashlib.sha256(raw).hexdigest() != request["sha256"]:
        raise ValueError("invalid source")
    if request["format"] != "pdf":
        with zipfile.ZipFile(io.BytesIO(raw)) as archive:
            entries = archive.infolist()
            if len(entries) > 1000 or sum(item.file_size for item in entries) > 64 << 20 or any(item.flag_bits & 1 for item in entries):
                raise ValueError("archive budget exceeded")
    if importlib.metadata.version("markitdown") != "0.1.3":
        raise ValueError("wrong converter version")
    socket.create_connection = denied
    socket.socket.connect = denied
    socket.socket.connect_ex = denied
    from markitdown import MarkItDown, StreamInfo
    from markitdown.converters import DocxConverter, XlsxConverter, PptxConverter, PdfConverter
    converters = {"docx": DocxConverter, "xlsx": XlsxConverter, "pptx": PptxConverter, "pdf": PdfConverter}
    converter = MarkItDown(enable_builtins=False, enable_plugins=False)
    converter.register_converter(converters[request["format"]]())
    result = converter.convert_stream(io.BytesIO(raw), stream_info=StreamInfo(extension="." + request["format"]))
    if not result.text_content or len(result.text_content.encode("utf-8")) > 4 << 20:
        raise ValueError("empty or oversized result")
    sys.stdout.buffer.write(json.dumps({"version": "markitdown/0.1.3", "markdown": result.text_content}, ensure_ascii=False).encode("utf-8"))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("document conversion rejected\n")
        sys.exit(1)

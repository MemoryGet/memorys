"""Single-file, stream-only MarkItDown 0.1.3 worker; no publishing or file paths."""
import base64
import hashlib
import importlib.metadata
import io
import json
import posixpath
import re
import socket
import sys
import zipfile
import xml.etree.ElementTree as ET


def denied(*args, **kwargs):
    raise PermissionError("network disabled")


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate field")
        result[key] = value
    return result


def xml_root(raw):
    if b"<!DOCTYPE" in raw or b"<!ENTITY" in raw or len(raw) > (8 << 20):
        raise ValueError("unsafe XML")
    return ET.fromstring(raw)


def xml_text(node, namespace):
    return "".join((item.text or "") for item in node.iter(namespace + "t"))


def structured_projection(archive, format):
    if format == "docx":
        root = xml_root(archive.read("word/document.xml"))
        w = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
        blocks = []
        paragraph = 0
        for node in root.iter(w + "p"):
            text = xml_text(node, w)
            paragraph += 1
            if text.strip():
                blocks.append({"text": text, "paragraph_path": [paragraph]})
        return blocks
    if format == "pptx":
        blocks = []
        names = sorted((n for n in archive.namelist() if re.fullmatch(r"ppt/slides/slide[0-9]+\.xml", n)), key=lambda n: int(re.search(r"slide([0-9]+)", n).group(1)))
        a = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
        for name in names:
            root = xml_root(archive.read(name))
            text = " ".join((node.text or "") for node in root.iter(a + "t")).strip()
            if text:
                blocks.append({"text": text, "slide": int(re.search(r"slide([0-9]+)", name).group(1))})
        return blocks
    if format != "xlsx":
        return []
    main = xml_root(archive.read("xl/workbook.xml"))
    rels = xml_root(archive.read("xl/_rels/workbook.xml.rels"))
    rel_ns = "{http://schemas.openxmlformats.org/package/2006/relationships}"
    rel_map = {item.attrib.get("Id"): item.attrib.get("Target", "") for item in rels}
    ss = []
    if "xl/sharedStrings.xml" in archive.namelist():
        shared = xml_root(archive.read("xl/sharedStrings.xml"))
        x = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
        ss = [xml_text(item, x) for item in shared.iter(x + "si")]
    x = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
    r = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"
    blocks = []
    for sheet in main.iter(x + "sheet"):
        name = sheet.attrib.get("name", "")
        target = rel_map.get(sheet.attrib.get(r + "id"), "")
        if not name or not target:
            continue
        path = posixpath.normpath(posixpath.join("xl", target.lstrip("/")))
        if path not in archive.namelist() or not path.startswith("xl/"):
            continue
        root = xml_root(archive.read(path))
        for cell in root.iter(x + "c"):
            ref = cell.attrib.get("r", "")
            value = cell.find(x + "v")
            text = ""
            if cell.attrib.get("t") == "s" and value is not None and value.text is not None:
                index = int(value.text)
                text = ss[index] if 0 <= index < len(ss) else ""
            elif cell.attrib.get("t") == "inlineStr":
                text = xml_text(cell, x)
            elif value is not None:
                text = value.text or ""
            if ref and text.strip():
                blocks.append({"text": text, "worksheet": name, "cell_range": ref})
    return blocks


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
    if request["format"] in {"docx", "xlsx", "pptx"}:
        try:
            with zipfile.ZipFile(io.BytesIO(raw)) as archive:
                blocks = structured_projection(archive, request["format"])
            if blocks:
                text = "\n\n".join(item["text"] for item in blocks)
                sys.stdout.buffer.write(json.dumps({"version": "markitdown/0.1.3", "markdown": text, "blocks": blocks}, ensure_ascii=False).encode("utf-8"))
                return
        except Exception:
            pass
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

"""Explicitly regenerate reviewed frozen content vectors; normal validation never writes them."""
import argparse
import copy
import json
from pathlib import Path

from canonical_content import canonical, frozen

ROOT = Path(__file__).resolve().parent


def samples():
    pdf = {"id": "blob-pdf", "sha256": "a" * 64, "size": 1000, "media_type": "application/pdf"}
    original = {"kind": "original", "blob_id": "blob-pdf", "locator": {"precision": "exact", "page": 1, "bbox": [0.125, 0, 1, 1]}}
    base = {"format": "akos.freeze/1", "blocks": [{"id": "block-1", "order": 0, "kind": "text",
             "text": "中文😀\n引文：<参数>，保留空格  ", "source_anchor": original}], "blobs": [pdf]}
    result = [("chinese_emoji_original", base)]
    manual = copy.deepcopy(base)
    manual["blocks"][0].update(text="人工修订：数值为0.00000001。", source_anchor={"kind": "revision", "revision_block_id": "block-1",
        "locator": {"precision": "coarse"}, "derived_from": [{"blob_id": "blob-pdf", "locator": {"precision": "exact", "page": 1}}]})
    result.append(("manual_revision", manual))
    for name, text in [("line_feed", "第一行\n第二行"), ("carriage_return_line_feed", "第一行\r\n第二行")]:
        sample = copy.deepcopy(base)
        sample["blocks"][0]["text"] = text
        result.append((name, sample))
    table = copy.deepcopy(base)
    table["blocks"][0].update(kind="table", text="参数\n精度 0.00000001", structured_content={"type": "table", "row_count": 2, "column_count": 2,
        "cells": [{"row": 0, "column": 0, "row_span": 1, "column_span": 2, "text": "参数", "role": "column_header"},
                  {"row": 1, "column": 0, "row_span": 1, "column_span": 1, "text": "精度", "role": "row_header"},
                  {"row": 1, "column": 1, "row_span": 1, "column_span": 1, "text": "0.00000001", "role": "data", "number_text": "0.00000001"}]})
    result.append(("merged_table_omitted_caption", table))
    caption = copy.deepcopy(table)
    caption["blocks"][0]["structured_content"]["caption"] = ""
    result.append(("merged_table_empty_caption", caption))
    image = {"id": "blob-image", "sha256": "b" * 64, "size": 100, "media_type": "image/png"}
    audio = {"id": "blob-audio", "sha256": "c" * 64, "size": 200, "media_type": "audio/wav"}
    media = {"format": "akos.freeze/1", "blobs": [audio, image], "blocks": []}
    for order, (kind, blob, payload) in enumerate([
        ("image", image, {"width": 100, "height": 50, "caption": "设备图片", "ocr_text": "型号A"}),
        ("audio", audio, {"duration_ms": 2000, "segments": [{"start_ms": 0, "end_ms": 1500, "text": "故障说明", "speaker_label": "甲"},
                                                          {"start_ms": 500, "end_ms": 1800, "text": "重叠语音", "speaker_label": "乙"}]})]):
        media["blocks"].append({"id": "block-" + kind, "order": order, "kind": kind, "text": "可审核媒体投影",
            "source_anchor": {"kind": "original", "blob_id": blob["id"], "locator": {"precision": "coarse"}},
            "structured_content": dict(payload, type=kind, blob=blob)})
    result.append(("image_and_overlapping_audio", media))
    return result


def build():
    cases = []
    for name, value in samples():
        data = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        cases.append({"name": name, "input_json": data.decode("utf-8"),
                      "canonical_json": canonical(data).decode("utf-8"), "hashes": frozen(data)})
    return {"format": "akos.freeze/1", "encoding": "AKOS-CJSON/1", "cases": cases}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    path = ROOT / "examples/frozen-content-golden.v1.json"
    content = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.write:
        path.write_text(content, encoding="utf-8")
    elif not path.exists() or path.read_text(encoding="utf-8") != content:
        raise SystemExit("Golden vectors differ; review changes before explicitly regenerating")
    else:
        print("PASS: golden vectors unchanged")

"""Local OpenAPI references and JSON Schema fixtures, not live API certification."""
import json
from pathlib import Path

from jsonschema import Draft202012Validator


def run():
    root = Path(__file__).resolve().parent
    spec = json.loads((root / "openapi/partner.v1.json").read_text())
    assert spec["openapi"] == "3.1.1"

    def refs(value):
        if isinstance(value, dict):
            if "$ref" in value:
                assert value["$ref"].startswith("#/"), "Only bundled references allowed"
                target = spec
                for key in value["$ref"][2:].split("/"):
                    target = target[key.replace("~1", "/").replace("~0", "~")]
            for child in value.values():
                refs(child)
        elif isinstance(value, list):
            for child in value:
                refs(child)

    refs(spec)
    schemas = spec["components"]["schemas"]
    for schema in schemas.values():
        Draft202012Validator.check_schema(schema)

    def validator(name):
        return Draft202012Validator({"$ref": f"#/components/schemas/{name}", "components": spec["components"]})

    anchor = {"kind": "revision", "revision_block_id": "block-1", "locator": {"precision": "coarse"}}
    citation = {"document_id": "doc-1", "revision_id": "rev-1", "block_id": "block-1", "source_anchor": anchor}
    trace = {"request_id": "req-1", "profile_version": 1, "profile_digest": "a" * 64, "timings": {}, "degraded": []}
    samples = {
        "SearchRequest": {"query": "产品说明", "space_id": "space-1", "kb_ids": []},
        "ReadRequest": {"document_id": "doc-1"},
        "AskRequest": {"question": "产品怎么使用", "space_id": "space-1", "stream": False},
        "FeedbackRequest": {"space_id": "space-1", "citation": citation, "kind": "incorrect", "comment": "请核对", "idempotency_key": "key-1"},
        "SearchResult": dict(trace, evidence=[{"citation": citation, "source_excerpt": "说明", "rank": 1}]),
        "ReadResult": dict(trace, document_id="doc-1", revision_id="rev-1", blocks=[], citations=[]),
        "AskResult": dict(trace, answer="证据不足", citations=[], status="insufficient_evidence"),
        "FeedbackResult": {"request_id": "req-1", "feedback_id": "feedback-1", "state": "received"},
    }
    for name, sample in samples.items():
        validator(name).validate(sample)
    blob = {"id": "blob-1", "sha256": "b" * 64, "size": 100, "media_type": "image/png"}
    table = {"type": "table", "row_count": 2, "column_count": 2, "cells": [
        {"row": 0, "column": 0, "row_span": 1, "column_span": 2, "text": "参数", "role": "column_header"},
        {"row": 1, "column": 0, "row_span": 1, "column_span": 1, "text": "精度", "role": "row_header"},
        {"row": 1, "column": 1, "row_span": 1, "column_span": 1, "text": "0.00000001", "number_text": "0.00000001", "role": "data"},
    ]}
    payloads = {"table": table, "image": {"type": "image", "blob": blob, "width": 100, "height": 50},
                "audio": {"type": "audio", "blob": dict(blob, media_type="audio/wav"), "duration_ms": 2000,
                          "segments": [{"start_ms": 0, "end_ms": 1500, "text": "语音文本"}]}}
    blocks = {}
    for kind, payload in payloads.items():
        blocks[kind] = {"id": "block-1", "revision_id": "rev-1", "kind": kind, "text": "检索投影", "source_anchor": anchor, "structured_content": payload}
        validator("Block").validate(blocks[kind])
    text_block = {"id": "block-1", "revision_id": "rev-1", "kind": "text", "text": "正文", "source_anchor": anchor}
    validator("Block").validate(text_block)
    failures = [
        ("SearchRequest", dict(samples["SearchRequest"], user_id="spoofed")),
        ("SearchRequest", dict(samples["SearchRequest"], top_k=101)),
        ("SearchRequest", dict(samples["SearchRequest"], kb_ids=None)),
        ("FeedbackRequest", dict(samples["FeedbackRequest"], target_request_id="req-1")),
        ("FeedbackRequest", {k: v for k, v in samples["FeedbackRequest"].items() if k != "citation"}),
        ("AskResult", dict(samples["AskResult"], backend_id="private")),
        ("SourceAnchor", dict(anchor, blob_id="inconsistent")),
        ("Locator", {"precision": "exact", "char_start": 1}),
        ("Block", dict(blocks["table"], structured_content=payloads["image"])),
        ("Block", {k: v for k, v in blocks["image"].items() if k != "structured_content"}),
        ("Block", dict(text_block, structured_content=payloads["table"])),
        ("ImageContent", dict(payloads["image"], public_url="https://example.invalid/private.png")),
        ("ImageContent", {k: v for k, v in payloads["image"].items() if k != "height"}),
        ("TableContent", dict(table, row_count=0)),
        ("TableCell", dict(table["cells"][0], row_span=0)),
        ("AudioSegment", {"start_ms": -1, "end_ms": 1, "text": "错误范围"}),
        ("Locator", {"precision": "exact", "bbox": [0, 0, 1, 1]}),
        ("BoundingBox", [0, 0, 1.5, 1]),
    ]
    for name, sample in failures:
        assert not validator(name).is_valid(sample), name
    for event in [
        {"type": "meta", "request_id": "req-1"}, {"type": "answer_delta", "text": "说明"},
        {"type": "citation", "citation": citation}, {"type": "done", "result": samples["AskResult"]},
        {"type": "error", "error": {"code": "publication_changed", "safe_message": "重新请求", "retryable": True, "request_id": "req-1"}},
    ]:
        validator("AnswerEvent").validate(event)
    ids = [op["post"]["operationId"] for op in spec["paths"].values()]
    assert set(ids) == {"knowledge_search", "knowledge_read", "knowledge_ask", "knowledge_feedback"}
    print(f"PASS: local refs, {len(schemas)} schemas, 8 DTO samples, 4 typed blocks, {len(failures)} rejected DTOs, 5 SSE payloads, 4 operation IDs")
    print("Not a full OpenAPI conformance validator; no live transport, authorization or source-alignment tests.")


if __name__ == "__main__":
    run()

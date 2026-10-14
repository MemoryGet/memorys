"""Read-only projection-package integrity; no provenance or live permission proof."""
import hashlib
import json
import os
from pathlib import Path
import stat
from jsonschema import Draft202012Validator
from canonical_content import canonical
from build_retrieval_evaluation import build
from retrieval_evaluation import _anchor, _covered, _fields, evaluate


def _hash(domain, value):
    raw = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8")
    return hashlib.sha256(domain + b"\x00" + canonical(raw)).hexdigest()


def package_digests(manifest, dataset):
    """Computes identity only; validate_package must check independent local bytes."""
    corpus = _hash(b"akos.retrieval-corpus/1", manifest)
    annotation = {k: v for k, v in dataset.items() if k != "annotation_digest"}
    annotation["corpus_digest"] = corpus
    return corpus, _hash(b"akos.retrieval-annotation/1", annotation)


def validate_package(manifest, dataset, local_files, max_total_bytes=256 << 20):
    _fields(manifest, ("format", "blocks"))
    if manifest["format"] != "akos.retrieval-corpus/1" or not isinstance(manifest["blocks"], list):
        raise ValueError("seal_manifest_invalid")
    if type(max_total_bytes) is not int or max_total_bytes < 0:
        raise ValueError("seal_budget_invalid")
    if not isinstance(local_files, dict) or len(manifest["blocks"]) > 10000:
        raise ValueError("seal_offline_inventory_invalid")
    lengths, declared = {}, 0
    for block in manifest["blocks"]:
        _fields(block, ("document_id", "revision_id", "block_id", "text_sha256", "byte_size", "codepoint_count"))
        key = tuple(block[k] for k in ("document_id", "revision_id", "block_id"))
        if any(not isinstance(k, str) or not k for k in key) or key in lengths:
            raise ValueError("seal_block_identity_invalid")
        if any(type(block[k]) is not int or not 0 <= block[k] <= 9223372036854775807 for k in ("byte_size", "codepoint_count")):
            raise ValueError("seal_integer_invalid")
        digest = block["text_sha256"]
        if not isinstance(digest, str) or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
            raise ValueError("seal_text_digest_invalid")
        lengths[key] = block["codepoint_count"]; declared += block["byte_size"]
    keys = [tuple(b[k] for k in ("document_id", "revision_id", "block_id")) for b in manifest["blocks"]]
    if keys != sorted(keys): raise ValueError("seal_inventory_order_invalid")
    if set(local_files) != set(lengths): raise ValueError("seal_inventory_coverage_mismatch")
    if declared > max_total_bytes: raise ValueError("seal_byte_budget_exceeded")
    for block, key in zip(manifest["blocks"], keys):
        # Paths are supplied explicitly by the operator; none come from the manifest.
        with Path(local_files[key]).open("rb") as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_size != block["byte_size"]:
                raise ValueError("seal_file_identity_invalid")
            raw = stream.read(block["byte_size"] + 1)
            after = os.fstat(stream.fileno())
            signature = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns)
            if signature(before) != signature(after) or signature(after) != signature(Path(local_files[key]).stat()):
                raise ValueError("seal_file_changed")
        if len(raw) != block["byte_size"] or hashlib.sha256(raw).hexdigest() != block["text_sha256"]:
            raise ValueError("seal_text_integrity_mismatch")
        text = raw.decode("utf-8", errors="strict")
        if len(text) != block["codepoint_count"]: raise ValueError("seal_codepoint_count_mismatch")
    schema = build()["retrieval-dataset.v1.schema.json"]
    Draft202012Validator(schema).validate(dataset)
    case_ids = set()
    for case in dataset["cases"]:
        if case["case_id"] in case_ids: raise ValueError("seal_duplicate_case")
        case_ids.add(case["case_id"])
        for anchor in case["allowed"] + case["relevant"]:
            key = _anchor(anchor)
            if key not in lengths or anchor["end"] > lengths[key]: raise ValueError("seal_anchor_outside_corpus")
        if (case["kind"] == "answerable") != bool(case["relevant"]): raise ValueError("seal_gold_kind_mismatch")
        gold = [tuple(a[k] for k in ("document_id", "revision_id", "block_id", "start", "end")) for a in case["relevant"]]
        if len(gold) != len(set(gold)): raise ValueError("seal_duplicate_gold")
        if any(not _covered(a, case["allowed"]) for a in case["relevant"]): raise ValueError("seal_gold_not_allowed")
    if not any(c["kind"] == "answerable" for c in dataset["cases"]):
        raise ValueError("seal_answerable_cases_required")
    corpus, annotation = package_digests(manifest, dataset)
    if (dataset["corpus_digest"], dataset["annotation_digest"]) != (corpus, annotation):
        raise ValueError("seal_package_digest_mismatch")
    return {"corpus_digest": corpus, "annotation_digest": annotation, "block_count": len(keys),
            "case_count": len(case_ids), "bytes_checked": declared, "formal_acceptance_passed": False,
            "unverified": ["original_document_provenance", "projection_mapping", "snapshot_authority", "independent_annotation",
                           "split_leakage", "runtime_retrieval", "post_check_immutability"]}


def evaluate_package(manifest, dataset, local_files, run, max_total_bytes=256 << 20):
    checked = validate_package(manifest, dataset, local_files, max_total_bytes)
    report = evaluate(dataset, run)
    report["package_integrity"] = checked
    return report

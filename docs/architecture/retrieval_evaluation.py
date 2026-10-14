"""Offline evidence Recall@10 arithmetic, not a real retrieval/permission benchmark."""
from fractions import Fraction
from jsonschema import Draft202012Validator
from build_retrieval_evaluation import build


def _fields(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError("evaluation_fields_invalid")


def _anchor(value):
    _fields(value, ("document_id", "revision_id", "block_id", "start", "end"))
    if any(not isinstance(value[k], str) or not value[k] for k in ("document_id", "revision_id", "block_id")):
        raise ValueError("evaluation_anchor_identity_invalid")
    if type(value["start"]) is not int or type(value["end"]) is not int or not 0 <= value["start"] < value["end"] <= 9223372036854775807:
        raise ValueError("evaluation_anchor_range_invalid")
    return tuple(value[k] for k in ("document_id", "revision_id", "block_id"))


def _covered(anchor, fragments):
    key = _anchor(anchor)
    intervals = sorted((f["start"], f["end"]) for f in fragments if _anchor(f) == key)
    cursor = anchor["start"]
    for start, end in intervals:
        if end <= cursor: continue
        if start > cursor: return False
        cursor = max(cursor, end)
        if cursor >= anchor["end"]: return True
    return False


def evaluate(dataset, run):
    """dataset is sealed independent gold and allowed ranges; run never supplies labels."""
    _fields(dataset, ("dataset_id", "corpus_digest", "annotation_digest", "cases"))
    _fields(run, ("dataset_id", "corpus_digest", "annotation_digest", "profile_digest", "implementation_id", "results"))
    for key in ("corpus_digest", "annotation_digest"):
        value = dataset[key]
        if not isinstance(value, str) or len(value) != 64 or any(c not in "0123456789abcdef" for c in value):
            raise ValueError("evaluation_digest_invalid")
    if not isinstance(dataset["dataset_id"], str) or not dataset["dataset_id"]:
        raise ValueError("evaluation_dataset_identity_invalid")
    if any(run[k] != dataset[k] for k in ("dataset_id", "corpus_digest", "annotation_digest")):
        raise ValueError("evaluation_dataset_binding_mismatch")
    if any(not isinstance(run[k], str) or not run[k] for k in ("profile_digest", "implementation_id")):
        raise ValueError("evaluation_run_identity_invalid")
    if len(run["profile_digest"]) != 64 or any(c not in "0123456789abcdef" for c in run["profile_digest"]):
        raise ValueError("evaluation_digest_invalid")
    if not isinstance(dataset["cases"], list) or not dataset["cases"] or not isinstance(run["results"], list):
        raise ValueError("evaluation_cases_required")
    cases = {}
    for case in dataset["cases"]:
        _fields(case, ("case_id", "kind", "request", "allowed", "relevant"))
        _fields(case["request"], ("query_text", "subject_id", "policy_snapshot_digest", "publication_snapshot_digest"))
        if not isinstance(case["case_id"], str) or not case["case_id"] or case["case_id"] in cases:
            raise ValueError("evaluation_duplicate_case")
        if case["kind"] not in {"answerable", "unanswerable", "denied", "withdrawn"}:
            raise ValueError("evaluation_kind_invalid")
        if not isinstance(case["allowed"], list) or not isinstance(case["relevant"], list):
            raise ValueError("evaluation_anchor_list_required")
        for anchor in case["allowed"] + case["relevant"]: _anchor(anchor)
        relevant = case["relevant"]
        if len({tuple(a[k] for k in ("document_id", "revision_id", "block_id", "start", "end")) for a in relevant}) != len(relevant):
            raise ValueError("evaluation_duplicate_gold")
        if (case["kind"] == "answerable") != bool(relevant):
            raise ValueError("evaluation_gold_kind_mismatch")
        if any(not _covered(a, case["allowed"]) for a in relevant):
            raise ValueError("evaluation_gold_not_allowed")
        cases[case["case_id"]] = case
    results = {}
    for result in run["results"]:
        _fields(result, ("case_id", "request", "status", "items"))
        if not isinstance(result["case_id"], str) or not result["case_id"]:
            raise ValueError("evaluation_case_identity_invalid")
        if result["case_id"] in results: raise ValueError("evaluation_duplicate_result")
        if result["status"] not in {"ok", "error", "timeout"} or not isinstance(result["items"], list):
            raise ValueError("evaluation_result_invalid")
        # Capture the final evidence window exactly, rather than truncating an oversized run.
        if len(result["items"]) > 10: raise ValueError("evaluation_top10_required")
        for item in result["items"]:
            _fields(item, ("item_id", "fragments"))
            if not isinstance(item["item_id"], str) or not item["item_id"] or not isinstance(item["fragments"], list) or not item["fragments"]:
                raise ValueError("evaluation_item_invalid")
            for a in item["fragments"]: _anchor(a)
        results[result["case_id"]] = result
    if set(results) != set(cases): raise ValueError("evaluation_case_coverage_mismatch")
    for key in cases:
        if results[key]["request"] != cases[key]["request"]:
            raise ValueError("evaluation_request_binding_mismatch")
    schemas = build()
    for name, value in (("retrieval-dataset.v1.schema.json", dataset), ("retrieval-run.v1.schema.json", run)):
        Draft202012Validator.check_schema(schemas[name])
        Draft202012Validator(schemas[name]).validate(value)
    scores, details, leakage, errors, negative_empty, negatives = [], [], 0, 0, 0, 0
    for key, case in cases.items():
        result = results[key]
        fragments = [a for item in result["items"] for a in item["fragments"]]
        leaked = sum(not _covered(a, case["allowed"]) for a in fragments)
        leakage += leaked
        if result["status"] != "ok": errors += 1
        # Failed calls cannot score from intermediate evidence. Leak checks still include it.
        scored = [a for a in fragments if _covered(a, case["allowed"])] if result["status"] == "ok" else []
        hits = sum(_covered(a, scored) for a in case["relevant"])
        if case["kind"] == "answerable":
            score = Fraction(hits, len(case["relevant"])); scores.append(score)
        else:
            score = None; negatives += 1
            negative_empty += int(result["status"] == "ok" and not result["items"])
        details.append({"case_id": key, "hits": hits, "relevant_count": len(case["relevant"]),
                        "recall": str(score) if score is not None else None, "leaked_fragments": leaked, "status": result["status"]})
    if not scores: raise ValueError("evaluation_answerable_cases_required")
    mean = sum(scores, Fraction()) / len(scores)
    return {"macro_recall_at_10": str(mean), "macro_recall_decimal": float(mean), "answerable_count": len(scores),
            "negative_count": negatives, "negative_empty_count": negative_empty, "failed_calls": errors,
            "leaked_fragments": leakage, "numeric_recall_target_met": mean >= Fraction(9, 10),
            "formal_acceptance_passed": False, "cases": details,
            "unverified": ["sealed_dataset_provenance", "real_authorization_and_publication", "retrieved_fragment_mapping",
                           "representative_chinese_corpus", "confidence_interval", "runtime_and_cost", "answer_quality"]}

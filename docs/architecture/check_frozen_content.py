"""Schema/order/blob reference checks and golden identities; no media/ownership verification."""
import json
from pathlib import Path

from jsonschema import Draft202012Validator

from canonical_content import canonical, frozen

ROOT = Path(__file__).resolve().parent


def validate_payload(value):
    schema = json.loads((ROOT / "schemas/frozen-content.v1.schema.json").read_text(encoding="utf-8"))
    Draft202012Validator(schema).validate(value)
    blobs = value["blobs"]
    ids = [blob["id"] for blob in blobs]
    if ids != sorted(set(ids)):
        raise ValueError("Frozen blobs must be unique and sorted by ID")
    indexed = {blob["id"]: blob for blob in blobs}
    blocks = value["blocks"]
    if len({block["id"] for block in blocks}) != len(blocks):
        raise ValueError("Duplicate frozen block ID")
    for order, block in enumerate(blocks):
        if block["order"] != order:
            raise ValueError("Frozen block order must equal array index")
        if block["kind"] == "table":
            cells = block["structured_content"]["cells"]
            coordinates = [(cell["row"], cell["column"]) for cell in cells]
            if coordinates != sorted(set(coordinates)):
                raise ValueError("Frozen table cells must be unique and sorted")
        if block["kind"] == "audio":
            segments = block["structured_content"]["segments"]
            times = [(segment["start_ms"], segment["end_ms"]) for segment in segments]
            if times != sorted(times):
                raise ValueError("Frozen audio segments must be stably ordered")
    check_blobs(value["blocks"], indexed)


def check_blobs(value, indexed):
    if isinstance(value, dict):
        if "blob_id" in value and value["blob_id"] not in indexed:
            raise ValueError("Source blob missing from frozen manifest")
        if "blob" in value and indexed.get(value["blob"]["id"]) != value["blob"]:
            raise ValueError("Media blob differs from frozen manifest")
        for child in value.values():
            check_blobs(child, indexed)
    elif isinstance(value, list):
        for child in value:
            check_blobs(child, indexed)


def run():
    fixtures = json.loads((ROOT / "examples/frozen-content-golden.v1.json").read_text(encoding="utf-8"))
    for sample in fixtures["cases"]:
        data = sample["input_json"].encode("utf-8")
        if canonical(data).decode("utf-8") != sample["canonical_json"]:
            raise AssertionError("Canonical byte mismatch: " + sample["name"])
        validate_payload(json.loads(sample["input_json"]))
        if frozen(data) != sample["hashes"]:
            raise AssertionError("Frozen hash mismatch: " + sample["name"])
    print("PASS:", len(fixtures["cases"]), "frozen schema/order/blob/golden cases")
    print("No source authorization, media authenticity, approval or publication tests")


if __name__ == "__main__":
    run()

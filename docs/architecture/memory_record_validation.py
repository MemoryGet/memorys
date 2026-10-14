"""Offline memory wire shape only; not an authorization/source proof."""
from datetime import datetime, timezone
from jsonschema import Draft202012Validator

from build_memory_record import build
from remote_wire_validation import finite_values, formats


def timestamp_ns(value):
    body = value[:-1]
    seconds, _, fraction = body.partition(".")
    point = datetime.fromisoformat(seconds).replace(tzinfo=timezone.utc)
    delta = point - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (delta.days * 86400 + delta.seconds) * 1000000000 + int(fraction.ljust(9, "0") or "0")


def validate(record):
    finite_values(record)
    Draft202012Validator(build(), format_checker=formats()).validate(record)
    if type(record["version"]) is not int:
        raise ValueError("Memory version must be an exact integer")
    # Explicit UTF-8 check rejects surrogate-containing decoded Python strings.
    def unicode_check(value):
        if isinstance(value, str):
            value.encode("utf-8", errors="strict")
        elif isinstance(value, dict):
            for child in value.values():
                unicode_check(child)
        elif isinstance(value, list):
            for child in value:
                unicode_check(child)
    unicode_check(record)
    if record["created_at"] == "0001-01-01T00:00:00Z":
        raise ValueError("Zero creation time is not a record timestamp")
    if "expires_at" in record and timestamp_ns(record["expires_at"]) <= timestamp_ns(record["created_at"]):
        raise ValueError("Expiry must follow creation")
    provenance = record["provenance"]
    if "task_id" in provenance and record.get("task_id") != provenance["task_id"]:
        raise ValueError("Task provenance must match record task scope")
    owners = {}
    for citation in provenance["citations"]:
        revision = citation["revision_id"]
        owner = citation["document_id"]
        if revision in owners and owners[revision] != owner:
            raise ValueError("Revision has inconsistent document owners")
        owners[revision] = owner
        anchor = citation["source_anchor"]
        for locator in [anchor["locator"]] + [a["locator"] for a in anchor.get("derived_from", [])]:
            if "char_start" in locator and locator["char_end"] < locator["char_start"]:
                raise ValueError("Reversed character range")
            if "bbox" in locator:
                left, top, right, bottom = locator["bbox"]
                if left >= right or top >= bottom:
                    raise ValueError("Reversed or empty bounding box")
    return {"shape_valid": True, "source_authenticated": False, "currently_authorized": False}

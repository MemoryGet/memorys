"""Read-only local byte checks; never copy/delete objects or switch storage bindings."""
import hashlib
import os
from pathlib import Path
import stat

from jsonschema import Draft202012Validator

from build_blob_copy_contract import build

HEADER_KEYS=("migration_id","source_mapping_id","target_mapping_id","source_lock_sha256","target_lock_sha256","retention_hold_id","snapshot")


def fingerprint_file(path,expected_size,expected_digest):
    """Explicit caller-supplied local path; never resolve a path from manifest text."""
    if not stat.S_ISREG(Path(path).stat().st_mode):raise ValueError("blob_copy_regular_file_required")
    with Path(path).open("rb") as stream:
        before=os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size!=expected_size:
            raise ValueError("blob_copy_file_size_mismatch")
        digest=hashlib.sha256();read=0
        while True:
            chunk=stream.read(1<<20)
            if not chunk:break
            read+=len(chunk)
            if read>expected_size:raise ValueError("blob_copy_file_grew")
            digest.update(chunk)
        after=os.fstat(stream.fileno());current=Path(path).stat()
        signature=lambda value:(value.st_dev,value.st_ino,value.st_size,value.st_mtime_ns)
        if signature(before)!=signature(after) or signature(after)!=signature(current):
            raise ValueError("blob_copy_file_changed")
        if read!=expected_size or digest.hexdigest()!=expected_digest:
            raise ValueError("blob_copy_file_digest_mismatch")


def validate_copy(manifest,local_files,expected,max_total_bytes=256<<20):
    """expected is independent frozen source inventory, not derived from manifest."""
    if not isinstance(local_files,dict) or len(local_files)>10000:raise ValueError("blob_copy_offline_object_limit")
    if isinstance(manifest,dict) and isinstance(manifest.get("objects"),list) and len(manifest["objects"])>10000:
        raise ValueError("blob_copy_offline_object_limit")
    if type(max_total_bytes) is not int or max_total_bytes<0:raise ValueError("blob_copy_byte_budget_invalid")
    schema=build();Draft202012Validator.check_schema(schema);Draft202012Validator(schema).validate(manifest)
    if type(manifest["schema_version"]) is not int:raise ValueError("blob_copy_integer_required")
    if not isinstance(expected,dict) or set(expected)!=set(HEADER_KEYS)|{"inventory"} or not isinstance(expected["inventory"],dict):
        raise ValueError("blob_copy_independent_inventory_required")
    if any(manifest[key]!=expected[key] for key in HEADER_KEYS):raise ValueError("blob_copy_snapshot_mismatch")
    if manifest["source_mapping_id"]==manifest["target_mapping_id"]:raise ValueError("blob_copy_identical_mapping")
    if len(expected["inventory"])>10000:raise ValueError("blob_copy_offline_object_limit")
    object_properties=schema["properties"]["objects"]["items"]["properties"]
    source_schema={"type":"object","additionalProperties":False,"required":["space_id","blob","source_binding_version"],
                   "properties":{key:object_properties[key] for key in ("space_id","blob","source_binding_version")}}
    for identity,source in expected["inventory"].items():
        Draft202012Validator(source_schema).validate(source)
        if source["blob"]["id"]!=identity:raise ValueError("blob_copy_expected_identity_mismatch")
        if type(source["blob"]["size"]) is not int or type(source["source_binding_version"]) is not int:
            raise ValueError("blob_copy_integer_required")
    blobs=set();entries=set();bindings=set();total=0
    for obj in manifest["objects"]:
        blob=obj["blob"];identity=blob["id"];entry=obj["entry_id"]
        if identity in blobs or entry in entries or obj["target_binding_ref"] in bindings:
            raise ValueError("blob_copy_duplicate_identity")
        blobs.add(identity);entries.add(entry);bindings.add(obj["target_binding_ref"])
        for value in (blob["size"],obj["source_binding_version"],obj["target_binding_version"]):
            if type(value) is not int:raise ValueError("blob_copy_integer_required")
        if obj["target_binding_version"]<=obj["source_binding_version"]:raise ValueError("blob_copy_binding_not_newer")
        source={"space_id":obj["space_id"],"blob":blob,"source_binding_version":obj["source_binding_version"]}
        if expected["inventory"].get(identity)!=source:raise ValueError("blob_copy_source_record_mismatch")
        total+=blob["size"]
        if total>max_total_bytes:raise ValueError("blob_copy_offline_byte_limit")
    if blobs!=set(expected["inventory"]) or entries!=set(local_files):raise ValueError("blob_copy_inventory_coverage_mismatch")
    # Check all metadata/budget before opening any local byte inputs.
    for obj in manifest["objects"]:
        fingerprint_file(local_files[obj["entry_id"]],obj["blob"]["size"],obj["blob"]["sha256"])
    return {"status":"offline_copy_bytes_validated","ready_for_binding_cutover":False,"object_count":len(blobs),"byte_count":total,
            "unverified":["trusted_source_snapshot","retention_hold","actual_target_binding_identity","immutable_target_objects",
                          "new_upload_and_purge_barrier","current_permissions_and_tombstones","atomic_mapping_switch",
                          "restart_binding_check","rollback_new_writes"]}

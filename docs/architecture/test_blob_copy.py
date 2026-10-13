"""Temporary local files test bytes only; no S3/provider or mapping migration."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from jsonschema import ValidationError

from blob_copy_validation import HEADER_KEYS,validate_copy


class BlobCopyTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.path=Path(self.temp.name)/"original.bin";self.path.write_bytes("中文原件😀".encode())
        data=self.path.read_bytes()
        self.manifest={"schema_version":1,"kind":"blob_store_copy","migration_id":"migration-1","source_mapping_id":"mapping-old",
                       "target_mapping_id":"mapping-new","source_lock_sha256":"a"*64,"target_lock_sha256":"b"*64,"retention_hold_id":"hold-1",
                       "snapshot":{"snapshot_id":"snapshot-1","journal_stream_id":"stream-1","watermark":"j1:"+"0"*64,"blob_writes":"paused"},
                       "objects":[{"entry_id":"entry-1","space_id":"space-1","blob":{"id":"blob-1","sha256":hashlib.sha256(data).hexdigest(),"size":len(data),"media_type":"application/octet-stream"},
                                   "source_binding_version":1,"target_binding_version":2,"target_binding_ref":"opaque-target-1"}]}
        self.expected={key:copy.deepcopy(self.manifest[key]) for key in HEADER_KEYS}
        self.expected["inventory"]={"blob-1":{key:copy.deepcopy(self.manifest["objects"][0][key]) for key in ("space_id","blob","source_binding_version")}}
        self.files={"entry-1":self.path}

    def test_matching_bytes_preserve_platform_identity_but_not_ready(self):
        before=self.path.read_bytes();report=validate_copy(self.manifest,self.files,self.expected)
        self.assertFalse(report["ready_for_binding_cutover"])
        self.assertEqual(report["object_count"],1);self.assertEqual(self.path.read_bytes(),before)

    def test_missing_and_extra_inventory_cannot_be_hidden(self):
        self.manifest["objects"]=[]
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):validate_copy(self.manifest,{},self.expected)
        self.setUp();self.files["extra"]=self.path
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):validate_copy(self.manifest,self.files,self.expected)

    def test_same_length_changed_bytes_and_truncation_rejected(self):
        original=self.path.read_bytes();self.path.write_bytes(b"x"*len(original))
        with self.assertRaisesRegex(ValueError,"digest_mismatch"):validate_copy(self.manifest,self.files,self.expected)
        self.path.write_bytes(original[:-1])
        with self.assertRaisesRegex(ValueError,"size_mismatch"):validate_copy(self.manifest,self.files,self.expected)

    def test_changed_blob_id_space_media_or_source_binding_rejected(self):
        baseline=copy.deepcopy(self.manifest)
        for mutation in (lambda x:x["objects"][0]["blob"].update(id="different"),lambda x:x["objects"][0].update(space_id="foreign"),lambda x:x["objects"][0]["blob"].update(media_type="text/plain"),lambda x:x["objects"][0].update(source_binding_version=2,target_binding_version=3)):
            candidate=copy.deepcopy(baseline);mutation(candidate)
            with self.assertRaisesRegex(ValueError,"source_record_mismatch"):validate_copy(candidate,self.files,self.expected)

    def test_duplicate_ids_and_nonincreasing_binding_rejected(self):
        self.manifest["objects"].append(copy.deepcopy(self.manifest["objects"][0]))
        with self.assertRaisesRegex(ValueError,"duplicate_identity"):validate_copy(self.manifest,self.files,self.expected)
        self.manifest["objects"].pop();self.manifest["objects"][0]["target_binding_version"]=1
        with self.assertRaisesRegex(ValueError,"binding_not_newer"):validate_copy(self.manifest,self.files,self.expected)

    def test_independent_snapshot_not_replaced_by_manifest_claim(self):
        self.manifest["snapshot"]["watermark"]="j1:"+"9"*64
        with self.assertRaisesRegex(ValueError,"snapshot_mismatch"):validate_copy(self.manifest,self.files,self.expected)

    def test_float_size_unknown_location_and_budget_rejected_before_io(self):
        self.manifest["objects"][0]["blob"]["size"]=float(self.path.stat().st_size)
        with self.assertRaisesRegex(ValueError,"integer_required"):validate_copy(self.manifest,self.files,self.expected)
        self.manifest["objects"][0]["blob"]["size"]=self.path.stat().st_size
        self.manifest["objects"][0]["download_url"]="https://invalid.example/private"
        with self.assertRaises(ValidationError):validate_copy(self.manifest,self.files,self.expected)
        self.manifest["objects"][0].pop("download_url");self.files["entry-1"]=Path(self.temp.name)/"does-not-exist"
        with self.assertRaisesRegex(ValueError,"byte_limit"):validate_copy(self.manifest,self.files,self.expected,max_total_bytes=0)

    def test_streaming_file_above_json_input_limit(self):
        with self.path.open("wb") as stream:
            for _ in range(8):stream.write(b"x"*(1<<20))
        blob=self.manifest["objects"][0]["blob"];blob["size"]=8<<20
        blob["sha256"]=hashlib.sha256(b"x"*(8<<20)).hexdigest()
        self.expected["inventory"]["blob-1"]["blob"]=copy.deepcopy(blob)
        self.assertEqual(validate_copy(self.manifest,self.files,self.expected)["byte_count"],8<<20)

    def test_empty_object_and_empty_inventory_are_explicit(self):
        self.path.write_bytes(b"");blob=self.manifest["objects"][0]["blob"]
        blob.update(size=0,sha256=hashlib.sha256(b"").hexdigest())
        self.expected["inventory"]["blob-1"]["blob"]=copy.deepcopy(blob)
        self.assertEqual(validate_copy(self.manifest,self.files,self.expected)["object_count"],1)
        self.manifest["objects"]=[];self.expected["inventory"]={}
        self.assertEqual(validate_copy(self.manifest,{},self.expected)["object_count"],0)

    def test_expected_source_inventory_requires_exact_identity_and_integers(self):
        self.expected["inventory"]["blob-1"]["source_binding_version"]=1.0
        with self.assertRaisesRegex(ValueError,"integer_required"):validate_copy(self.manifest,self.files,self.expected)
        self.expected["inventory"]["blob-1"]["source_binding_version"]=1
        self.expected["inventory"]["different-id"]=self.expected["inventory"].pop("blob-1")
        with self.assertRaisesRegex(ValueError,"expected_identity_mismatch"):validate_copy(self.manifest,self.files,self.expected)

    def test_detected_file_change_during_read_rejected(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        original=self.path.stat()
        names=("st_dev","st_ino","st_size","st_mtime_ns","st_mode")
        changed=SimpleNamespace(**{name:getattr(original,name) for name in names})
        changed.st_mtime_ns+=1
        with patch("blob_copy_validation.os.fstat",side_effect=[original,changed]):
            with self.assertRaisesRegex(ValueError,"file_changed"):validate_copy(self.manifest,self.files,self.expected)


if __name__=="__main__":unittest.main()

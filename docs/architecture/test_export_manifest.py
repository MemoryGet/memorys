"""Synthetic full inventory tests; no real database export/import."""
import copy
import hashlib
import json
import unittest

from jsonschema import ValidationError

from build_export_manifest import RESOURCE_TYPES
from export_validation import validate_package


class ExportManifestTests(unittest.TestCase):
    def setUp(self):
        self.payloads = {f"entry-{resource}": b"" for resource in RESOURCE_TYPES}
        self.manifest = {"schema_version": 1, "export_id": "export-1", "kind": "metadata_full_snapshot",
                         "source": {"deployment_id": "deployment-1", "build_lock_sha256": "a"*64, "metadata_contract_version": "1.0"},
                         "snapshot": {"snapshot_id": "snapshot-1", "journal_stream_id": "stream-1", "committed_watermark": "cursor-1", "boundary": "after_complete_transaction", "writes": "all_metadata_writes_paused"},
                         "segments": [{"entry_id": key, "resource_type": resource, "record_contract": f"akos.metadata.{resource}/1", "encoding": "jsonl-envelope/1", "record_count": 0, "byte_length": 0, "sha256": hashlib.sha256(b"").hexdigest()} for resource, key in zip(RESOURCE_TYPES,self.payloads)],
                         "blob_payloads": "external_required_before_cutover", "derived_indexes": "excluded_rebuild_or_verify", "lease_restore": "invalidate_all_inflight"}

    def set_payload(self, index, data):
        segment = self.manifest["segments"][index]
        self.payloads[segment["entry_id"]] = data
        segment["byte_length"] = len(data)
        segment["sha256"] = hashlib.sha256(data).hexdigest()
        segment["record_count"] = data.count(b"\n")

    def test_full_empty_inventory_is_manifest_valid_only(self):
        report = validate_package(self.manifest, self.payloads)
        self.assertFalse(report["ready_for_import"])
        self.assertEqual(report["segment_count"], len(RESOURCE_TYPES))
        self.assertIn("complete_authoritative_snapshot", report["unverified"])

    def test_missing_required_resource_not_hidden_by_equal_segment_count(self):
        original = self.manifest["segments"][0]
        original.update(resource_type="principal", record_contract="akos.metadata.principal/1")
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):
            validate_package(self.manifest,self.payloads)

    def test_duplicate_entry_and_extra_or_missing_payload_rejected(self):
        baseline = copy.deepcopy(self.manifest)
        self.manifest["segments"][1]["entry_id"] = self.manifest["segments"][0]["entry_id"]
        with self.assertRaisesRegex(ValueError,"duplicate_entry"):
            validate_package(self.manifest,self.payloads)
        self.manifest = baseline
        self.payloads["extra"] = b""
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):
            validate_package(self.manifest,self.payloads)
        self.payloads.pop("extra"); self.payloads.pop("entry-space")
        with self.assertRaisesRegex(ValueError,"missing_payload"):
            validate_package(self.manifest,self.payloads)

    def test_bytes_and_record_count_tampering_rejected(self):
        self.payloads["entry-space"] = b"changed"
        with self.assertRaisesRegex(ValueError,"digest_mismatch"):
            validate_package(self.manifest,self.payloads)
        self.set_payload(0,b'{"record_type":"space","record_id":"s1","payload":{}}\n')
        self.manifest["segments"][0]["record_count"] = 2
        with self.assertRaisesRegex(ValueError,"count_mismatch"):
            validate_package(self.manifest,self.payloads)

    def test_multiple_segments_supported_but_duplicate_record_rejected(self):
        data=b'{"record_type":"space","record_id":"s1","payload":{}}\n'
        self.set_payload(0,data)
        segment=copy.deepcopy(self.manifest["segments"][0]);segment["entry_id"]="entry-space-2"
        self.manifest["segments"].append(segment);self.payloads["entry-space-2"]=data
        with self.assertRaisesRegex(ValueError,"duplicate_record"):
            validate_package(self.manifest,self.payloads)
        self.set_payload(len(self.manifest["segments"])-1,data.replace(b's1',b's2'))
        self.assertEqual(validate_package(self.manifest,self.payloads)["record_count"],2)

    def test_wrong_contract_or_envelope_type_rejected(self):
        self.manifest["segments"][0]["record_contract"]="akos.metadata.principal/1"
        with self.assertRaisesRegex(ValueError,"contract_mismatch"):
            validate_package(self.manifest,self.payloads)
        self.manifest["segments"][0]["record_contract"]="akos.metadata.space/1"
        self.set_payload(0,b'{"record_type":"principal","record_id":"s1","payload":{}}\n')
        with self.assertRaisesRegex(ValueError,"envelope_invalid"):
            validate_package(self.manifest,self.payloads)

    def test_duplicate_json_keys_blank_lines_and_missing_newline_rejected(self):
        for data in (b'{"record_type":"space","record_id":"s1","record_id":"s2","payload":{}}\n',b'\n',b'{}',b'{}\r\n'):
            self.set_payload(0,data)
            with self.assertRaises(ValueError):
                validate_package(self.manifest,self.payloads)

    def test_float_counters_partial_snapshot_and_unknown_fields_rejected(self):
        self.manifest["segments"][0]["record_count"]=0.0
        with self.assertRaisesRegex(ValueError,"integer_required"):
            validate_package(self.manifest,self.payloads)
        self.manifest["segments"][0]["record_count"]=0
        for mutation in (lambda x:x["snapshot"].update(writes="some_writes_paused"),lambda x:x.update(download_url="https://invalid.example"),lambda x:x.update(lease_restore="keep_active")):
            candidate=copy.deepcopy(self.manifest);mutation(candidate)
            with self.assertRaises(ValidationError):
                validate_package(candidate,self.payloads)

    def test_unicode_and_nonfinite_values_checked_inside_payload(self):
        row={"record_type":"space","record_id":"s1","payload":{"title":"中文😀"}}
        self.set_payload(0,(json.dumps(row)+"\n").encode("utf-8"))
        self.assertEqual(validate_package(self.manifest,self.payloads)["record_count"],1)
        for data in (b'{"record_type":"space","record_id":"s1","payload":{"title":"\\ud800"}}\n',
                     b'{"record_type":"space","record_id":"s1","payload":{"x":NaN}}\n',
                     b'{"record_type":"space","record_id":"s1","payload":{"x":1e10000}}\n'):
            self.set_payload(0,data)
            with self.assertRaises((ValueError,UnicodeError)):
                validate_package(self.manifest,self.payloads)


if __name__=="__main__":
    unittest.main()

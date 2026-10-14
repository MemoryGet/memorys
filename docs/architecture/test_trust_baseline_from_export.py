"""Synthetic complete exports and journal fixtures; no real snapshot source."""
import copy
import hashlib
import json
import unittest
import test_export_manifest as export_fixtures
import test_journal_trust_baseline as journal_fixtures
from trust_baseline_from_export import build_baseline,validate_journal_from_export


class TrustExportBaselineTests(unittest.TestCase):
    def setUp(self):
        e=export_fixtures.ExportManifestTests();e.setUp();self.manifest=e.manifest;self.payloads=e.payloads
        j=journal_fixtures.JournalTrustBaselineTests();j.setUp();self.j=j.f
        self.manifest['snapshot']['committed_watermark']=self.j.f.expected['from_cursor']
        entry=next(s for s in self.manifest['segments'] if s['resource_type']=='trust_head')
        data=json.dumps({'record_type':'trust_head','record_id':'doc-1','payload':self.j.baseline['heads']['doc-1']}).encode()+b'\n'
        self.payloads[entry['entry_id']]=data;entry.update(record_count=1,byte_length=len(data),sha256=hashlib.sha256(data).hexdigest())
        self.raw=json.dumps(self.manifest,separators=(',',':')).encode()
        self.expected={'export_id':self.manifest['export_id'],'deployment_id':self.manifest['source']['deployment_id'],
                       'build_lock_sha256':self.manifest['source']['build_lock_sha256'],'metadata_contract_version':'1.1',
                       'snapshot_id':self.manifest['snapshot']['snapshot_id'],'journal_stream_id':self.manifest['snapshot']['journal_stream_id'],
                       'committed_watermark':self.manifest['snapshot']['committed_watermark'],'manifest_sha256':hashlib.sha256(self.raw).hexdigest()}

    def test_export_bound_baseline_drives_existing_head_journal(self):
        baseline,source=build_baseline(self.raw,self.payloads,self.expected)
        self.assertEqual(baseline,self.j.baseline);self.assertEqual(source['typed_head_count'],1)
        report=validate_journal_from_export(self.raw,self.payloads,self.expected,self.j.f.bundle,self.j.f.payloads,self.j.f.expected)
        self.assertFalse(report['ready_for_replay']);self.assertEqual(report['initial_trust_head_count'],1)

    def test_independent_identity_fields_and_exact_manifest_hash_reject_changes(self):
        for key in self.expected:
            e=copy.deepcopy(self.expected);e[key]='different'
            with self.subTest(key=key),self.assertRaisesRegex(ValueError,'identity_mismatch'):build_baseline(self.raw,self.payloads,e)
        # Formatting alone changes the raw file identity; do not silently normalize it.
        with self.assertRaisesRegex(ValueError,'identity_mismatch'):build_baseline(self.raw+b' ',self.payloads,self.expected)

    def test_payload_tampering_rejected_and_returned_baseline_detached(self):
        baseline,source=build_baseline(self.raw,self.payloads,self.expected)
        baseline['heads']['doc-1']['row_version']=999
        fresh,_=build_baseline(self.raw,self.payloads,self.expected);self.assertEqual(fresh['heads']['doc-1']['row_version'],8)
        entry='entry-trust_head';self.payloads[entry]=self.payloads[entry].replace(b'"row_version": 8',b'"row_version": 7')
        with self.assertRaisesRegex(ValueError,'payload_digest_mismatch'):build_baseline(self.raw,self.payloads,self.expected)

    def test_export_commit_cursor_must_match_journal_start(self):
        self.j.f.expected['from_cursor']='j1:'+'f'*64
        self.j.f.bundle['from_cursor']=self.j.f.expected['from_cursor']
        with self.assertRaisesRegex(ValueError,'snapshot_boundary_mismatch'):
            validate_journal_from_export(self.raw,self.payloads,self.expected,self.j.f.bundle,self.j.f.payloads,self.j.f.expected)


if __name__=='__main__':unittest.main()

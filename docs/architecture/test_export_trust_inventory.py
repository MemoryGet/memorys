"""Expanded inventory/envelope checks only, not typed trust restore authority."""
import copy
import hashlib
import json
import unittest
from jsonschema import ValidationError
import test_export_manifest as export_fixtures
import test_journal_contract as journal_fixtures
from build_export_manifest import ADDED_RESOURCE_TYPES,RESOURCE_TYPES
from export_validation import validate_package
from journal_validation import validate_bundle


class ExportTrustInventoryTests(unittest.TestCase):
    def setUp(self):
        f=export_fixtures.ExportManifestTests();f.setUp();self.manifest=f.manifest;self.payloads=f.payloads

    def test_new_families_required_even_if_all_empty(self):
        report=validate_package(self.manifest,self.payloads)
        self.assertFalse(report['ready_for_import'])
        for kind in ADDED_RESOURCE_TYPES:
            m=copy.deepcopy(self.manifest);p=copy.deepcopy(self.payloads)
            entry=next(s for s in m['segments'] if s['resource_type']==kind)
            m['segments'].remove(entry);p.pop(entry['entry_id'])
            duplicate=copy.deepcopy(m['segments'][0]);duplicate['entry_id']='replacement-'+kind
            m['segments'].append(duplicate);p[duplicate['entry_id']]=b''
            with self.subTest(kind=kind),self.assertRaisesRegex(ValueError,'coverage_mismatch'):validate_package(m,p)

    def test_old_version_cannot_claim_extended_snapshot(self):
        self.manifest['source']['metadata_contract_version']='1.0'
        with self.assertRaises(ValidationError):validate_package(self.manifest,self.payloads)

    def test_new_envelopes_are_counted_but_not_typed_payload_verified(self):
        for kind in ADDED_RESOURCE_TYPES:
            entry=next(s for s in self.manifest['segments'] if s['resource_type']==kind)
            payload={'synthetic_claim':True}
            if kind=='trust_head':payload={'domain_id':'record-trust_head','issuer_id':'issuer','active':{'id':'snap','version':1,'digest':'a'*64},'build_lock_digest':'b'*64,'high_water_version':1,'row_version':1}
            row=json.dumps({'record_type':kind,'record_id':'record-'+kind,'payload':payload}).encode()+b'\n'
            self.payloads[entry['entry_id']]=row
            entry.update(record_count=1,byte_length=len(row),sha256=hashlib.sha256(row).hexdigest())
        report=validate_package(self.manifest,self.payloads)
        self.assertEqual(report['record_count'],len(ADDED_RESOURCE_TYPES));self.assertFalse(report['ready_for_import'])
        self.assertIn('typed_record_payloads',report['unverified'])

    def test_new_journal_families_preserve_hash_chain_without_proving_replay(self):
        for kind in ADDED_RESOURCE_TYPES:
            f=journal_fixtures.JournalContractTests();f.setUp()
            tx=f.bundle['transactions'][0];mutation=tx['mutations'][0]
            mutation['record_type']=kind;entry=mutation['payload']['entry_id']
            data=json.loads(f.payloads[entry]);data['record_type']=kind
            if kind=='trust_head':
                data['payload']={'domain_id':'doc-1','issuer_id':'issuer','active':{'id':'snap','version':1,'digest':'a'*64},'build_lock_digest':'b'*64,'high_water_version':1,'row_version':1}
            raw=json.dumps(data).encode();f.payloads[entry]=raw
            mutation['payload'].update(sha256=hashlib.sha256(raw).hexdigest(),byte_length=len(raw));f.rehash()
            with self.subTest(kind=kind):
                baseline={'snapshot_id':'snapshot-1','journal_stream_id':f.expected['journal_stream_id'],'from_cursor':f.expected['from_cursor'],'heads':{}} if kind=='trust_head' else None
                report=validate_bundle(f.bundle,f.payloads,f.expected,baseline)
                self.assertFalse(report['ready_for_replay'])


if __name__=='__main__':unittest.main()

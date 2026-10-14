"""Typed Head sequence only; no durable replay, provenance or source snapshot."""
import copy
import hashlib
import json
import unittest
import test_journal_contract as fixtures
from journal_validation import validate_bundle


class JournalTrustHeadTests(unittest.TestCase):
    def setUp(self):
        self.f=fixtures.JournalContractTests();self.f.setUp()
        for index,tx in enumerate(self.f.bundle['transactions']):
            change=tx['mutations'][0];change.update(record_type='trust_head',effect='upsert')
            head={'domain_id':'doc-1','issuer_id':'issuer','active':{'id':'snapshot-'+str(index),'version':9007199254740993+index,'digest':'a'*64},
                  'build_lock_digest':'b'*64,'high_water_version':9007199254740993+index,'row_version':index+1}
            self.replace(index,head)
        self.f.rehash()
        self.baseline={'snapshot_id':'snapshot-1','journal_stream_id':self.f.expected['journal_stream_id'],'from_cursor':self.f.expected['from_cursor'],'heads':{}}
    def replace(self,index,head):
        change=self.f.bundle['transactions'][index]['mutations'][0];entry=change['payload']['entry_id']
        row={'record_type':change['record_type'],'record_id':change['record_id'],'effect':change['effect'],'payload':head}
        raw=json.dumps(row).encode();self.f.payloads[entry]=raw
        change['payload'].update(sha256=hashlib.sha256(raw).hexdigest(),byte_length=len(raw))
    def head(self,index):
        entry=self.f.bundle['transactions'][index]['mutations'][0]['payload']['entry_id']
        return json.loads(self.f.payloads[entry])['payload']
    def check(self):
        self.f.rehash();return validate_bundle(self.f.bundle,self.f.payloads,self.f.expected,self.baseline)

    def test_typed_sequence_counts_but_does_not_prove_replay(self):
        report=self.check();self.assertEqual(report['typed_mutation_counts'],{'trust_head':2})
        self.assertFalse(report['ready_for_replay']);self.assertIn('snapshot_initial_versions',report['unverified'])

    def test_payload_version_and_mutation_disagreement_rejected_even_rehashed(self):
        h=self.head(1);h['row_version']=3;self.replace(1,h)
        with self.assertRaisesRegex(ValueError,'trust_head_version_mismatch'):self.check()
        self.setUp();self.f.bundle['transactions'][0]['mutations'][0].update(new_version=2)
        h=self.head(0);h['row_version']=2;self.replace(0,h)
        with self.assertRaisesRegex(ValueError,'trust_head_version_mismatch'):self.check()

    def test_internal_high_water_rollback_and_issuer_change_rejected(self):
        for kind in ('same','lower','issuer'):
            self.setUp();h=self.head(1)
            if kind=='issuer':h['issuer_id']='other'
            else:h['active']['version']=9007199254740993-(kind=='lower');h['high_water_version']=h['active']['version']
            self.replace(1,h)
            with self.subTest(kind=kind),self.assertRaisesRegex(ValueError,'trust_head_(issuer_changed|high_water_not_advancing)'):self.check()

    def test_tombstone_cannot_erase_trust_high_water(self):
        change=self.f.bundle['transactions'][1]['mutations'][0];change['effect']='tombstone'
        self.replace(1,self.head(1))
        with self.assertRaisesRegex(ValueError,'tombstone_forbidden'):self.check()


if __name__=='__main__':unittest.main()

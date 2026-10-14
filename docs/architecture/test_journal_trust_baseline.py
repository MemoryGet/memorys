"""Independent synthetic baseline comparisons, not trusted snapshot provenance."""
import copy
import unittest
import test_journal_trust_head as fixtures
from journal_validation import validate_bundle


class JournalTrustBaselineTests(unittest.TestCase):
    def setUp(self):
        self.f=fixtures.JournalTrustHeadTests();self.f.setUp()
        # Convert the original two mutations to advances from a frozen existing Head.
        baseline=copy.deepcopy(self.f.head(0));baseline['row_version']=8
        baseline['active']['version']=9007199254740992;baseline['high_water_version']=9007199254740992
        self.f.baseline['heads']={'doc-1':baseline}
        for index,tx in enumerate(self.f.f.bundle['transactions']):
            tx['mutations'][0].update(previous_version=8+index,new_version=9+index)
            head=self.f.head(index);head['row_version']=9+index;self.f.replace(index,head)
        self.f.f.rehash()

    def test_existing_head_advance_and_input_snapshot_not_mutated(self):
        before=copy.deepcopy(self.f.baseline);report=self.f.check()
        self.assertEqual(report['initial_trust_head_count'],1);self.assertEqual(before,self.f.baseline)
        self.assertFalse(report['ready_for_replay'])

    def test_missing_baseline_missing_record_and_mismatched_boundary(self):
        args=(self.f.f.bundle,self.f.f.payloads,self.f.f.expected)
        with self.assertRaisesRegex(ValueError,'snapshot_required'):validate_bundle(*args)
        self.f.baseline['heads']={}
        with self.assertRaisesRegex(ValueError,'snapshot_missing_record'):self.f.check()
        self.setUp();self.f.baseline['from_cursor']='j1:'+'f'*64
        with self.assertRaisesRegex(ValueError,'snapshot_boundary_mismatch'):self.f.check()

    def test_first_record_must_match_snapshot_row_and_high_water(self):
        self.f.baseline['heads']['doc-1']['row_version']=7
        with self.assertRaisesRegex(ValueError,'snapshot_version_mismatch'):self.f.check()
        self.setUp();h=self.f.head(0);h['active']['version']=9007199254740992;h['high_water_version']=9007199254740992;self.f.replace(0,h)
        with self.assertRaisesRegex(ValueError,'high_water_not_advancing'):self.f.check()

    def test_false_bootstrap_cannot_hide_existing_head(self):
        tx=self.f.f.bundle['transactions'][0];tx['mutations'][0].update(previous_version=None,new_version=1)
        head=self.f.head(0);head['row_version']=1;self.f.replace(0,head)
        with self.assertRaisesRegex(ValueError,'snapshot_version_mismatch'):self.f.check()


if __name__=='__main__':unittest.main()

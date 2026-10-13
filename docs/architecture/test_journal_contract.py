"""Synthetic journal chain tests; no claim of real commit-order or replay safety."""
import copy
import hashlib
import json
import unittest

from journal_validation import transaction_cursor,validate_bundle


class JournalContractTests(unittest.TestCase):
    def setUp(self):
        self.payloads={};self.start="j1:"+"0"*64
        self.bundle={"schema_version":1,"journal_stream_id":"stream-1","from_cursor":self.start,
                     "through_cursor":self.start,"hash_format":"akos.journal/1","transactions":[]}
        self.append("tx-allocated-later-committed-first","upsert",None,1)
        self.append("tx-allocated-first-committed-later","tombstone",1,2)
        self.expected={key:self.bundle[key] for key in ("journal_stream_id","from_cursor","through_cursor")}

    def append(self,txid,effect,old,new):
        entry="payload-"+txid
        data=json.dumps({"record_type":"document","record_id":"doc-1","effect":effect,"payload":{"reason":"synthetic-only"}}).encode()
        self.payloads[entry]=data
        change={"ordinal":0,"operation_id":"op-"+txid,"record_type":"document","record_id":"doc-1",
                "previous_version":old,"new_version":new,"effect":effect,"payload":{"entry_id":entry,"sha256":hashlib.sha256(data).hexdigest(),"byte_length":len(data)}}
        tx={"transaction_id":txid,"journal_stream_id":"stream-1","previous_cursor":self.bundle["through_cursor"],"mutation_count":1,"mutations":[change]}
        tx["cursor"]=transaction_cursor(tx);self.bundle["transactions"].append(tx);self.bundle["through_cursor"]=tx["cursor"]

    def rehash(self):
        previous=self.start
        for tx in self.bundle["transactions"]:
            tx["previous_cursor"]=previous;tx["cursor"]=transaction_cursor(tx);previous=tx["cursor"]
        self.bundle["through_cursor"]=previous
        self.expected["through_cursor"]=previous

    def test_parent_chain_allows_ids_unrelated_to_allocation_order(self):
        report=validate_bundle(self.bundle,self.payloads,self.expected)
        self.assertFalse(report["ready_for_replay"])
        self.assertEqual(report["transaction_count"],2)
        self.assertIn("actual_atomic_commit_order",report["unverified"])

    def test_missing_reordered_and_truncated_transactions_rejected(self):
        baseline=copy.deepcopy(self.bundle)
        for transactions in ([baseline["transactions"][1]],list(reversed(baseline["transactions"])),[baseline["transactions"][0]]):
            self.bundle=copy.deepcopy(baseline);self.bundle["transactions"]=transactions
            with self.assertRaises(ValueError):validate_bundle(self.bundle,self.payloads,self.expected)

    def test_self_declared_shorter_head_cannot_override_trusted_head(self):
        self.bundle["transactions"].pop();self.bundle["through_cursor"]=self.bundle["transactions"][0]["cursor"]
        with self.assertRaisesRegex(ValueError,"expected_boundary_mismatch"):
            validate_bundle(self.bundle,self.payloads,self.expected)

    def test_partial_transaction_count_and_ordinals_rejected(self):
        self.bundle["transactions"][0]["mutation_count"]=2;self.rehash()
        with self.assertRaisesRegex(ValueError,"incomplete_transaction"):
            validate_bundle(self.bundle,self.payloads,self.expected)
        self.bundle["transactions"][0]["mutation_count"]=1
        self.bundle["transactions"][0]["mutations"][0]["ordinal"]=1;self.rehash()
        with self.assertRaisesRegex(ValueError,"ordinal_mismatch"):
            validate_bundle(self.bundle,self.payloads,self.expected)

    def test_cross_stream_and_repeated_transaction_ids_rejected(self):
        self.bundle["transactions"][1]["journal_stream_id"]="foreign";self.rehash()
        with self.assertRaisesRegex(ValueError,"chain_boundary_mismatch"):
            validate_bundle(self.bundle,self.payloads,self.expected)
        self.bundle["transactions"][1]["journal_stream_id"]="stream-1"
        self.bundle["transactions"][1]["transaction_id"]=self.bundle["transactions"][0]["transaction_id"];self.rehash()
        with self.assertRaisesRegex(ValueError,"duplicate_transaction"):
            validate_bundle(self.bundle,self.payloads,self.expected)

    def test_version_chain_and_duplicate_resource_rejected(self):
        self.bundle["transactions"][1]["mutations"][0]["previous_version"]=0;self.rehash()
        with self.assertRaisesRegex(ValueError,"version_chain_mismatch"):
            validate_bundle(self.bundle,self.payloads,self.expected)
        self.bundle["transactions"][1]["mutations"][0]["previous_version"]=1
        tx=self.bundle["transactions"][0];duplicate=copy.deepcopy(tx["mutations"][0]);duplicate["ordinal"]=1
        tx["mutations"].append(duplicate);tx["mutation_count"]=2;self.rehash()
        with self.assertRaisesRegex(ValueError,"duplicate_resource"):
            validate_bundle(self.bundle,self.payloads,self.expected)

    def test_payload_tamper_missing_and_extra_payload_rejected(self):
        key=next(iter(self.payloads));data=self.payloads[key];self.payloads[key]=data+b" "
        with self.assertRaisesRegex(ValueError,"digest_mismatch"):validate_bundle(self.bundle,self.payloads,self.expected)
        self.payloads.pop(key)
        with self.assertRaisesRegex(ValueError,"missing_payload"):validate_bundle(self.bundle,self.payloads,self.expected)
        self.payloads[key]=data;self.payloads["extra"]=b""
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):validate_bundle(self.bundle,self.payloads,self.expected)

    def test_cursor_covers_effect_and_payload_reference(self):
        self.bundle["transactions"][1]["mutations"][0]["effect"]="upsert"
        with self.assertRaisesRegex(ValueError,"cursor_digest_mismatch"):validate_bundle(self.bundle,self.payloads,self.expected)
        self.rehash()
        with self.assertRaisesRegex(ValueError,"payload_target_mismatch"):validate_bundle(self.bundle,self.payloads,self.expected)

    def test_empty_range_requires_identical_boundaries_and_no_payloads(self):
        self.bundle["transactions"]=[];self.bundle["through_cursor"]=self.start;self.expected["through_cursor"]=self.start
        self.assertEqual(validate_bundle(self.bundle,{},self.expected)["transaction_count"],0)
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):validate_bundle(self.bundle,self.payloads,self.expected)

    def test_versions_above_float_exact_range_remain_exact(self):
        first=self.bundle["transactions"][0]["mutations"][0]
        second=self.bundle["transactions"][1]["mutations"][0]
        first["new_version"]=9007199254740993
        second["previous_version"]=first["new_version"];second["new_version"]=first["new_version"]+1
        self.rehash()
        self.assertEqual(validate_bundle(self.bundle,self.payloads,self.expected)["mutation_count"],2)
        second["previous_version"]-=1;self.rehash()
        with self.assertRaisesRegex(ValueError,"version_chain_mismatch"):validate_bundle(self.bundle,self.payloads,self.expected)

    def test_integral_float_version_and_nonincreasing_version_rejected(self):
        change=self.bundle["transactions"][1]["mutations"][0]
        change["new_version"]=2.0;self.rehash()
        with self.assertRaisesRegex(ValueError,"integer_required"):validate_bundle(self.bundle,self.payloads,self.expected)
        change["new_version"]=1;self.rehash()
        with self.assertRaisesRegex(ValueError,"version_not_increasing"):validate_bundle(self.bundle,self.payloads,self.expected)


if __name__=="__main__":unittest.main()

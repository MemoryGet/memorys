"""Real temporary projection files, synthetic labels; no real benchmark."""
import copy
import hashlib
from pathlib import Path
import tempfile
import unittest
import test_retrieval_evaluation as fixtures
from retrieval_seal import package_digests, validate_package, evaluate_package


class RetrievalSealTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.path=Path(self.temp.name)/'projection.txt'; self.text='维护周期😀为30天。'+'中文'*50
        raw=self.text.encode(); self.path.write_bytes(raw)
        f=fixtures.RetrievalEvaluationTests(); f.setUp(); self.data=f.data
        self.manifest={'format':'akos.retrieval-corpus/1','blocks':[{'document_id':'doc-1','revision_id':'rev-1','block_id':'block-1',
                       'text_sha256':hashlib.sha256(raw).hexdigest(),'byte_size':len(raw),'codepoint_count':len(self.text)}]}
        self.files={('doc-1','rev-1','block-1'):self.path}; self.seal()

    def seal(self):
        self.data['corpus_digest'],self.data['annotation_digest']=package_digests(self.manifest,self.data)

    def test_valid_utf8_package_remains_unaccepted(self):
        r=validate_package(self.manifest,self.data,self.files)
        self.assertEqual(r['bytes_checked'],len(self.text.encode())); self.assertFalse(r['formal_acceptance_passed'])

    def test_changed_bytes_without_new_hash_rejected(self):
        raw=self.path.read_bytes(); self.path.write_bytes(b'x'+raw[1:])
        with self.assertRaisesRegex(ValueError,'text_integrity_mismatch'):validate_package(self.manifest,self.data,self.files)

    def test_query_scope_gold_and_corpus_change_stales_digest(self):
        for mutate in (lambda d:d['cases'][0]['request'].update(query_text='另一个问题'),
                       lambda d:d['cases'][0]['request'].update(policy_snapshot_digest='f'*64),
                       lambda d:d['cases'][0]['relevant'][0].update(end=9)):
            d=copy.deepcopy(self.data);mutate(d)
            with self.assertRaisesRegex(ValueError,'package_digest_mismatch'):validate_package(self.manifest,d,self.files)
        self.manifest['blocks'][0]['codepoint_count']-=1
        with self.assertRaisesRegex(ValueError,'codepoint_count_mismatch'):validate_package(self.manifest,self.data,self.files)

    def test_codepoint_ranges_not_utf8_offsets_or_missing_blocks(self):
        self.data['cases'][0]['relevant'][0]['end']=len(self.text.encode())
        self.seal()
        with self.assertRaisesRegex(ValueError,'anchor_outside_corpus'):validate_package(self.manifest,self.data,self.files)
        self.setUp();self.data['cases'][0]['relevant'][0]['revision_id']='old';self.seal()
        with self.assertRaisesRegex(ValueError,'anchor_outside_corpus'):validate_package(self.manifest,self.data,self.files)

    def test_inventory_coverage_budget_and_exact_sizes(self):
        with self.assertRaisesRegex(ValueError,'coverage_mismatch'):validate_package(self.manifest,self.data,{})
        with self.assertRaisesRegex(ValueError,'budget_exceeded'):validate_package(self.manifest,self.data,self.files,1)
        self.manifest['blocks'][0]['byte_size']=float(self.manifest['blocks'][0]['byte_size'])
        with self.assertRaisesRegex(ValueError,'integer_invalid'):validate_package(self.manifest,self.data,self.files)

    def test_canonical_key_order_stable_but_crlf_distinct(self):
        self.assertEqual(package_digests(self.manifest,self.data),package_digests(dict(reversed(list(self.manifest.items()))),self.data))
        first=package_digests(self.manifest,self.data)
        self.data['cases'][0]['request']['query_text']='第一行\n第二行'; a=package_digests(self.manifest,self.data)
        self.data['cases'][0]['request']['query_text']='第一行\r\n第二行'; b=package_digests(self.manifest,self.data)
        self.assertNotEqual(a,b);self.assertNotEqual(first,a)

    def test_resealed_duplicate_gold_and_disallowed_evidence_rejected(self):
        self.data['cases'][0]['relevant']*=2;self.seal()
        with self.assertRaisesRegex(ValueError,'duplicate_gold'):validate_package(self.manifest,self.data,self.files)
        self.setUp();self.data['cases'][0]['allowed']=[];self.seal()
        with self.assertRaisesRegex(ValueError,'gold_not_allowed'):validate_package(self.manifest,self.data,self.files)

    def test_package_evaluation_requires_run_binding_to_checked_bytes(self):
        f=fixtures.RetrievalEvaluationTests(); f.setUp(); run=f.run
        with self.assertRaisesRegex(ValueError,'dataset_binding_mismatch'):evaluate_package(self.manifest,self.data,self.files,run)
        run.update(corpus_digest=self.data['corpus_digest'],annotation_digest=self.data['annotation_digest'])
        report=evaluate_package(self.manifest,self.data,self.files,run)
        self.assertEqual(report['macro_recall_at_10'],'1');self.assertFalse(report['formal_acceptance_passed'])


if __name__=='__main__':unittest.main()

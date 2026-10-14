"""Synthetic anchors prove arithmetic only, never real Recall or authority."""
import copy
import unittest
from retrieval_evaluation import evaluate


def anchor(start=0, end=10, revision="rev-1", block="block-1"):
    return {"document_id": "doc-1", "revision_id": revision, "block_id": block, "start": start, "end": end}


class RetrievalEvaluationTests(unittest.TestCase):
    def setUp(self):
        self.request = {"query_text": "设备维护周期是多少？", "subject_id": "user-1", "policy_snapshot_digest": "d"*64,
                        "publication_snapshot_digest": "e"*64}
        self.data = {"dataset_id": "sealed-1", "corpus_digest": "a"*64, "annotation_digest": "b"*64,
                     "cases": [{"case_id": "q1", "kind": "answerable", "request": copy.deepcopy(self.request), "allowed": [anchor(0, 100)], "relevant": [anchor()]}]}
        self.run = {k: self.data[k] for k in ("dataset_id", "corpus_digest", "annotation_digest")}
        self.run.update(profile_digest="c"*64, implementation_id="adapter-1", results=[{"case_id": "q1", "request": copy.deepcopy(self.request), "status": "ok",
                        "items": [{"item_id": "item-1", "fragments": [anchor()]}]}])

    def test_correct_exact_coverage_never_formal_acceptance(self):
        r=evaluate(self.data,self.run)
        self.assertEqual(r["macro_recall_at_10"],"1"); self.assertTrue(r["numeric_recall_target_met"])
        self.assertFalse(r["formal_acceptance_passed"])

    def test_partial_ranges_gaps_and_union_are_distinct(self):
        for fragments, expected in [([anchor(0,5)],"0"),([anchor(0,5),anchor(5,10)],"1"),([anchor(0,4),anchor(5,10)],"0")]:
            self.run["results"][0]["items"][0]["fragments"]=fragments
            self.assertEqual(evaluate(self.data,self.run)["macro_recall_at_10"],expected)

    def test_old_revision_wrong_block_and_out_of_scope_are_leaks(self):
        for bad in (anchor(revision="old"),anchor(block="other"),anchor(90,110)):
            self.run["results"][0]["items"][0]["fragments"]=[bad]
            r=evaluate(self.data,self.run); self.assertEqual(r["macro_recall_at_10"],"0"); self.assertEqual(r["leaked_fragments"],1)

    def test_macro_denominator_does_not_drop_timeout_or_negative_cases(self):
        self.data["cases"].append({"case_id":"q2","request":copy.deepcopy(self.request),"kind":"answerable","allowed":[anchor(0,100)],"relevant":[anchor()]})
        self.run["results"].append({"case_id":"q2","request":copy.deepcopy(self.request),"status":"timeout","items":[]})
        for kind in ("unanswerable","denied","withdrawn"):
            self.data["cases"].append({"case_id":kind,"request":copy.deepcopy(self.request),"kind":kind,"allowed":[],"relevant":[]})
            self.run["results"].append({"case_id":kind,"request":copy.deepcopy(self.request),"status":"ok","items":[]})
        r=evaluate(self.data,self.run); self.assertEqual(r["macro_recall_at_10"],"1/2")
        self.assertEqual((r["answerable_count"],r["negative_count"],r["failed_calls"]),(2,3,1))
        self.run["results"][1]["items"]=[{"item_id":"partial-before-timeout","fragments":[anchor()]}]
        self.assertEqual(evaluate(self.data,self.run)["macro_recall_at_10"],"1/2")

    def test_duplicate_results_gold_missing_case_and_binding_rejected(self):
        baseline=copy.deepcopy(self.run)
        self.run["results"]*=2
        with self.assertRaisesRegex(ValueError,"duplicate_result"):evaluate(self.data,self.run)
        self.run=copy.deepcopy(baseline);self.run["results"]=[]
        with self.assertRaisesRegex(ValueError,"coverage_mismatch"):evaluate(self.data,self.run)
        self.run=copy.deepcopy(baseline);self.run["corpus_digest"]="d"*64
        with self.assertRaisesRegex(ValueError,"binding_mismatch"):evaluate(self.data,self.run)
        self.run=copy.deepcopy(baseline);self.data["cases"][0]["relevant"]*=2
        with self.assertRaisesRegex(ValueError,"duplicate_gold"):evaluate(self.data,self.run)

    def test_duplicates_do_not_inflate_hits_and_overlong_window_rejected(self):
        self.run["results"][0]["items"]*=10
        r=evaluate(self.data,self.run);self.assertEqual(r["cases"][0]["hits"],1)
        self.run["results"][0]["items"].append(copy.deepcopy(self.run["results"][0]["items"][0]))
        with self.assertRaisesRegex(ValueError,"top10_required"):evaluate(self.data,self.run)

    def test_exact_integer_and_disallowed_gold_rejected(self):
        self.data["cases"][0]["relevant"][0]["end"]=10.0
        with self.assertRaisesRegex(ValueError,"range_invalid"):evaluate(self.data,self.run)
        self.setUp(); self.data["cases"][0]["relevant"]=[anchor(revision="old")]
        with self.assertRaisesRegex(ValueError,"gold_not_allowed"):evaluate(self.data,self.run)


if __name__=="__main__":unittest.main()

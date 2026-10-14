"""Schema and request-binding tests, not sealed corpus or real backend evidence."""
import copy
import json
from pathlib import Path
import unittest
from jsonschema import Draft202012Validator, ValidationError
import test_retrieval_evaluation as fixtures
from build_retrieval_evaluation import build
from retrieval_evaluation import evaluate


class RetrievalSchemaTests(unittest.TestCase):
    def setUp(self):
        f=fixtures.RetrievalEvaluationTests(); f.setUp(); self.data=f.data; self.run=f.run

    def test_saved_schemas_match_generator_and_validate_samples(self):
        for name, schema in build().items():
            Draft202012Validator.check_schema(schema)
            saved=json.loads((Path(__file__).parent/'schemas'/name).read_text(encoding='utf-8'))
            self.assertEqual(saved,schema)
            Draft202012Validator(saved).validate(self.data if 'dataset' in name else self.run)

    def test_query_subject_and_two_snapshots_cannot_change(self):
        for field in ('query_text','subject_id','policy_snapshot_digest','publication_snapshot_digest'):
            run=copy.deepcopy(self.run)
            run['results'][0]['request'][field]='f'*64 if field.endswith('digest') else 'different'
            with self.subTest(field=field), self.assertRaisesRegex(ValueError,'request_binding_mismatch'):
                evaluate(self.data,run)

    def test_matching_but_invalid_request_claims_are_not_accepted(self):
        for field,value in [('query_text',''),('subject_id',''),('policy_snapshot_digest','not-a-digest'),('publication_snapshot_digest',12)]:
            data=copy.deepcopy(self.data); run=copy.deepcopy(self.run)
            data['cases'][0]['request'][field]=value; run['results'][0]['request'][field]=value
            with self.subTest(field=field), self.assertRaises(ValidationError): evaluate(data,run)

    def test_unknown_provider_fields_and_uri_anchors_rejected(self):
        self.run['results'][0]['items'][0]['download_url']='https://invalid.example/private'
        with self.assertRaisesRegex(ValueError,'fields_invalid'):evaluate(self.data,self.run)
        self.setUp();self.data['cases'][0]['request']['provider_filter']={}
        with self.assertRaisesRegex(ValueError,'fields_invalid'):evaluate(self.data,self.run)

    def test_unicode_query_is_exact_and_not_implicitly_normalized(self):
        self.data['cases'][0]['request']['query_text']='Ａ型设备😀'
        self.run['results'][0]['request']['query_text']='A型设备😀'
        with self.assertRaisesRegex(ValueError,'request_binding_mismatch'):evaluate(self.data,self.run)
        self.run['results'][0]['request']['query_text']='Ａ型设备😀'
        self.assertFalse(evaluate(self.data,self.run)['formal_acceptance_passed'])


if __name__=='__main__':unittest.main()

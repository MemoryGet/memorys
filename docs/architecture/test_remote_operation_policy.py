"""Action-shaped fixtures only; no live policy, pipeline, job or token authority."""
import copy
import unittest
from remote_operation_policy import policy,check_operation
from build_operation_grant_golden import build
import json


class RemoteOperationPolicyTests(unittest.TestCase):
    def test_all_seventeen_golden_scopes_match_declared_operation(self):
        rules=policy();self.assertEqual(len(rules),17)
        for case in build()['cases']:
            scope=json.loads(case['scope_json'])
            self.assertFalse(check_operation(case['operation'],case['method'],case['path'],scope)['authorized'])

    def test_read_grant_cannot_invoke_delete_verify_or_cancel(self):
        rules=policy();scope={'space_id':'s','kb_ids':['kb'],'action':'knowledge.search'}
        for name in ('blob_store_delete','index_writer_verify','index_writer_delete','control_cancel'):
            with self.subTest(name=name),self.assertRaisesRegex(ValueError,'action_mismatch'):
                check_operation(name,'POST',rules[name]['path'],scope)

    def test_pipeline_actions_do_not_imply_generic_model_access(self):
        rules=policy();scope={'space_id':'s','kb_ids':['kb'],'action':'knowledge.search'}
        self.assertFalse(check_operation('embedder_embed','POST',rules['embedder_embed']['path'],scope)['authorized'])
        with self.assertRaisesRegex(ValueError,'action_mismatch'):
            check_operation('generator_generate','POST',rules['generator_generate']['path'],scope)
        scope['action']='index.prepare'
        with self.assertRaisesRegex(ValueError,'action_mismatch'):
            check_operation('reranker_rerank','POST',rules['reranker_rerank']['path'],scope)

    def test_unknown_route_and_empty_scope_never_widen(self):
        rules=policy();scope={'space_id':'s','kb_ids':[],'action':'knowledge.ask'}
        with self.assertRaisesRegex(ValueError,'bounded_scope'):check_operation('generator_generate','POST',rules['generator_generate']['path'],scope)
        with self.assertRaisesRegex(ValueError,'route_mismatch'):check_operation('generator_generate','GET',rules['generator_generate']['path'],scope)
        with self.assertRaisesRegex(ValueError,'unknown'):check_operation('shell_run','POST','/modules/v1/shell/run',scope)


if __name__=='__main__':unittest.main()

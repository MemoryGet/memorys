"""All-operation shape/digest coverage, not authorization action semantics."""
import json
from pathlib import Path
import unittest
from build_operation_grant_golden import build
from build_remote_contract import build as spec
from access_grant_validation import binding_digest


class OperationGrantGoldenTests(unittest.TestCase):
    def test_saved_suite_covers_exact_current_operation_inventory(self):
        saved=json.loads((Path(__file__).parent/'examples/operation-grant-golden.v1.json').read_text(encoding='utf-8'))
        self.assertEqual(saved,build())
        operations={v['post']['operationId']:p for p,v in spec()['paths'].items()}
        self.assertEqual({c['operation'] for c in saved['cases']},set(operations))
        for case in saved['cases']:
            self.assertEqual(case['path'],operations[case['operation']]);self.assertEqual(case['method'],'POST')

    def test_input_is_bound_and_scope_is_extracted_from_same_envelope(self):
        for case in build()['cases']:
            with self.subTest(operation=case['operation']):
                self.assertNotEqual(case['request_digest'],case['mutated_request_digest'])
                request=json.loads(case['request_json']);scope=json.dumps(request['context']['scope'],ensure_ascii=False).encode()
                self.assertEqual(binding_digest(scope,'scope'),case['scope_digest'])


if __name__=='__main__':unittest.main()

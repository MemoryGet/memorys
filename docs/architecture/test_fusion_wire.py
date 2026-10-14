"""New provenance DTO checks, no actual retriever/re-ranker backend."""
import copy,json,subprocess,tempfile
from pathlib import Path
import unittest
from canonical_content import canonical
from check_retrieval_wire_mapping import fixtures
from check_parser_wire_mapping import PROJECT,invoke
from build_remote_contract import build
from remote_wire_validation import validate
from jsonschema import ValidationError


class FusionWireTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();cls.exe=Path(cls.temp.name)/'fusion-wire.exe'
        subprocess.run([str(PROJECT/'.tools/go/bin/go.exe'),'build','-o',str(cls.exe),'./cmd/architecture-parser-wire'],cwd=PROJECT,check=True,capture_output=True,timeout=60)
    @classmethod
    def tearDownClass(cls):cls.temp.cleanup()
    def fixture(self):
        value=copy.deepcopy(fixtures()[2]);candidate=value['request']['input']['candidates'][0]
        candidate['generation_ids']=['gen-1','keyword-gen']
        candidate['fusion_origins']=[{'route_id':'route-1','retriever_id':'retriever-a','rank':1,'generation_ids':['gen-1']},
                                     {'route_id':'keyword-route','retriever_id':'keyword-a','rank':2,'generation_ids':['keyword-gen']}]
        return value
    def test_origins_schema_and_actual_go_roundtrip(self):
        value=self.fixture();spec=build();validate(spec,'reranker_rerank_Request',value['request'])
        result=invoke(self.exe,value);self.assertEqual(result.returncode,0,result.stderr)
        output=json.loads(result.stdout);validate(spec,'reranker_rerank_Request',output['request'])
        self.assertEqual(canonical(result.stdout.encode()),canonical(json.dumps({k:value[k] for k in ('request','response')},ensure_ascii=False).encode()))
    def test_forged_duplicate_origins_missing_generation_and_raw_retriever_rejected(self):
        for mutation in (lambda c:c['fusion_origins'][1].update(route_id='route-1'),lambda c:c['fusion_origins'][1].update(generation_ids=['foreign']),lambda c:c['fusion_origins'].pop()):
            value=self.fixture();mutation(value['request']['input']['candidates'][0]);self.assertNotEqual(invoke(self.exe,value).returncode,0)
        value=copy.deepcopy(fixtures()[1]);c=value['response']['result']['candidates'][0]
        c['fusion_origins']=[{'route_id':'route-1','retriever_id':'retriever-a','rank':1,'generation_ids':['gen-1']}]
        with self.assertRaises(ValidationError):validate(build(),'retriever_search_Response',value['response'])
        self.assertNotEqual(invoke(self.exe,value).returncode,0)


if __name__=='__main__':unittest.main()

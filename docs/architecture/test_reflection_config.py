import copy
import json
from pathlib import Path
import unittest
from jsonschema import Draft202012Validator
from check_assembly_semantics import fixtures

class ReflectionConfigTests(unittest.TestCase):
    def test_plan_query_bound_is_explicit_and_closed(self):
        config,_=fixtures();v=Draft202012Validator(json.loads((Path(__file__).parent/'schemas/deployment-config.v1.schema.json').read_text(encoding='utf-8')))
        self.assertTrue(v.is_valid(config))
        for value in (0,5,True,2.5):
            c=copy.deepcopy(config);c['query_profiles'][0]['reflection']['max_plan_queries']=value
            self.assertFalse(v.is_valid(c))
        c=copy.deepcopy(config);del c['query_profiles'][0]['reflection']['max_plan_queries']
        self.assertFalse(v.is_valid(c))

if __name__=='__main__':unittest.main()

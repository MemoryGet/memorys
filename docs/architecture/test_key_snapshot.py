"""Offline public snapshot hashing; neither provenance nor real deployment trust."""
import copy
import json
from pathlib import Path
import unittest
from jsonschema import ValidationError
from build_key_snapshot import build,golden,inspect_snapshot


class KeySnapshotTests(unittest.TestCase):
    def setUp(self):self.sample=golden();self.config=json.loads(self.sample['config_json'])
    def inspect(self,value):return inspect_snapshot(json.dumps(value).encode())

    def test_saved_schema_and_large_integer_identity(self):
        root=Path(__file__).parent
        self.assertEqual(json.loads((root/'schemas/key-snapshot.v1.schema.json').read_text(encoding='utf-8')),build())
        self.assertEqual(json.loads((root/'examples/key-snapshot-golden.v1.json').read_text(encoding='utf-8')),self.sample)
        self.assertEqual(self.inspect(self.config),self.sample['expected']);self.assertEqual(self.sample['expected']['version'],9007199254740993)

    def test_float_duplicate_key_and_invalid_window_rejected(self):
        self.config['version']=1.0
        with self.assertRaisesRegex(ValueError,'integer_required'):self.inspect(self.config)
        self.setUp();self.config['keys'].append(copy.deepcopy(self.config['keys'][0]))
        with self.assertRaisesRegex(ValueError,'duplicate_kid'):self.inspect(self.config)
        self.setUp();self.config['keys'][0]['sign_until_ns']=0
        with self.assertRaisesRegex(ValueError,'window_invalid'):self.inspect(self.config)

    def test_noncanonical_base64_and_provider_url_rejected(self):
        self.config['keys'][0]['public_key']=self.config['keys'][0]['public_key'][:-2]+'9='
        with self.assertRaisesRegex(ValueError,'base64_invalid'):self.inspect(self.config)
        self.setUp();self.config['keys'][0]['download_url']='https://invalid.example/key'
        with self.assertRaises(ValidationError):self.inspect(self.config)

    def test_version_and_window_bind_digest(self):
        first=self.inspect(self.config);self.config['version']+=1
        self.assertNotEqual(first['digest'],self.inspect(self.config)['digest'])
        self.setUp();self.config['valid_until_ns']-=1
        self.assertNotEqual(first['digest'],self.inspect(self.config)['digest'])


if __name__=='__main__':unittest.main()

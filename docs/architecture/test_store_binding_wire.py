import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from store_binding_validation import validate_bundle
from test_store_binding import fixture


class StoreWireTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.root=Path(__file__).resolve().parents[2]
        cls.tmp=tempfile.TemporaryDirectory();cls.exe=Path(cls.tmp.name)/'store-binding.exe'
        r=subprocess.run([str(cls.root/'.tools/go/bin/go.exe'),'build','-o',str(cls.exe),'./cmd/architecture-store-binding'],cwd=cls.root,capture_output=True)
        if r.returncode: raise RuntimeError('store binding Go inspector build failed')

    @classmethod
    def tearDownClass(cls):cls.tmp.cleanup()

    def invoke(self,raw):return subprocess.run([str(self.exe)],input=raw,capture_output=True)

    def test_actual_go_roundtrip_preserves_closed_dto_and_uint64(self):
        b=fixture();b['expectation']['binding_id']='绑定😀'
        for o in b['observations']:o['binding_id']=b['expectation']['binding_id']
        validate_bundle(b)
        r=self.invoke(json.dumps(b,ensure_ascii=False).encode());self.assertEqual(r.returncode,0,r.stderr)
        actual=json.loads(r.stdout);self.assertEqual(actual,b)
        self.assertFalse(validate_bundle(actual)['ready_for_service'])

    def test_go_rejects_closed_keys_identity_and_receipt_conflicts(self):
        for mutate in (lambda b:b.update(extra=1),lambda b:b['expectation']['identity'].update(binding_version=1.0),
                       lambda b:b['expectation']['identity'].update(BindingVersion=b['expectation']['identity'].pop('binding_version')),
                       lambda b:b['observations'][0]['identity'].update(metadata_store_epoch_id='old'),
                       lambda b:b['observations'][1].update(observation_id=b['observations'][0]['observation_id']),
                       lambda b:b['observations'].pop()):
            b=copy.deepcopy(fixture());mutate(b)
            with self.assertRaises(ValueError):validate_bundle(b)
            self.assertNotEqual(self.invoke(json.dumps(b).encode()).returncode,0)

    def test_go_rejects_raw_duplicate_noninteger_overflow_and_unicode(self):
        raw=json.dumps(fixture()).encode()
        for bad in (raw.replace(b'"schema_version": 1',b'"schema_version": 1, "schema_version": 1'),
                    raw.replace(b'"schema_version": 1',b'"schema_version": 1e0'),
                    raw.replace(b'18446744073709551615',b'18446744073709551616'),
                    raw.replace(b'"binding"',b'"\\ud800"'),raw+b'{}'):
            self.assertNotEqual(self.invoke(bad).returncode,0)


if __name__=='__main__':unittest.main()

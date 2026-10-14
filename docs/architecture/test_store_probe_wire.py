import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from test_store_probe import fixture,NOW
from store_probe_validation import validate_probe

class ProbeWireTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.root=Path(__file__).resolve().parents[2];cls.tmp=tempfile.TemporaryDirectory();cls.exe=Path(cls.tmp.name)/'probe.exe'
        result=subprocess.run([str(cls.root/'.tools/go/bin/go.exe'),'build','-o',str(cls.exe),'./cmd/architecture-store-probe'],cwd=cls.root,capture_output=True)
        if result.returncode:raise RuntimeError('Go bound-probe inspector build failed')
    @classmethod
    def tearDownClass(cls):cls.tmp.cleanup()
    def invoke(self,b,now=NOW):return subprocess.run([str(self.exe),str(now)],input=json.dumps(b,ensure_ascii=False).encode(),capture_output=True)

    def test_actual_go_preserves_nanoseconds_and_uint64(self):
        b=fixture();r=self.invoke(b);self.assertEqual(r.returncode,0,r.stderr)
        actual=json.loads(r.stdout);self.assertEqual(actual,b)
        self.assertFalse(validate_probe(actual,NOW)['ready_for_service'])

    def test_actual_go_rejects_window_identity_and_raw_integer_changes(self):
        for mutate in (lambda b:b['observation']['binding'].update(probe_id='old'),lambda b:b['request']['scope'].update(domain_id='wrong'),
                       lambda b:b['request']['binding'].update(policy_version=1.0),lambda b:b['observation'].update(observed_at_ns=NOW+1),
                       lambda b:b['request']['request'].update(deadline_ns=9223372036854775808),lambda b:b.update(extra=1)):
            b=fixture();mutate(b)
            with self.assertRaises(ValueError):validate_probe(b,NOW)
            self.assertNotEqual(self.invoke(b).returncode,0)
        b=fixture();self.assertNotEqual(self.invoke(b,b['request']['binding']['deadline_ns']).returncode,0)

    def test_raw_json_alias_exponent_and_duplicate_rejected(self):
        raw=json.dumps(fixture()).encode()
        for bad in (raw.replace(b'"schema_version": 1',b'"schema_version": 1e0'),raw.replace(b'"schema_version": 1',b'"schema_version": 1, "schema_version": 1'),
                    raw.replace(b'"observed_at_ns"',b'"ObservedAtNS"'),raw+b'{}'):
            r=subprocess.run([str(self.exe),str(NOW)],input=bad,capture_output=True)
            self.assertNotEqual(r.returncode,0)

if __name__=='__main__':unittest.main()

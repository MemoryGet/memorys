"""Typed metadata evidence only; no provenance, retired keys or database import."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from jsonschema import ValidationError
from canonical_content import canonical
from build_trust_head import build,validate_record
from export_validation import validate_package
import test_export_manifest as fixtures


def head():
    return {'domain_id':'domain-1','issuer_id':'issuer','active':{'id':'snapshot','version':9007199254740993,'digest':'a'*64},
            'build_lock_digest':'b'*64,'high_water_version':9007199254740993,'row_version':18446744073709551615}


class TrustHeadRecordTests(unittest.TestCase):
    def setUp(self):
        f=fixtures.ExportManifestTests();f.setUp();self.manifest=f.manifest;self.payloads=f.payloads
    def payload(self,payload,record_id='domain-1'):
        entry=next(s for s in self.manifest['segments'] if s['resource_type']=='trust_head')
        data=json.dumps({'record_type':'trust_head','record_id':record_id,'payload':payload}).encode()+b'\n'
        self.payloads[entry['entry_id']]=data;entry.update(record_count=1,byte_length=len(data),sha256=hashlib.sha256(data).hexdigest())
    def test_valid_typed_record_count_and_saved_schema(self):
        self.assertEqual(json.loads((Path(__file__).parent/'schemas/trust-head.v1.schema.json').read_text(encoding='utf-8')),build())
        self.payload(head());report=validate_package(self.manifest,self.payloads)
        self.assertEqual(report['typed_record_counts'],{'trust_head':1});self.assertFalse(report['ready_for_import'])
    def test_export_rejects_high_water_record_id_float_and_unknown_field(self):
        for mutate in (lambda h:h.update(high_water_version=1),lambda h:h.update(row_version=1.0),lambda h:h.update(provider_url='https://invalid.example'),lambda h:h['active'].update(version=0)):
            h=head();mutate(h);self.payload(h)
            with self.assertRaises((ValueError,ValidationError)):validate_package(self.manifest,self.payloads)
        self.payload(head(),'foreign')
        with self.assertRaisesRegex(ValueError,'record_id_mismatch'):validate_package(self.manifest,self.payloads)
    def test_actual_go_typed_roundtrip_and_invalid_mutations(self):
        root=Path(__file__).parent.parent.parent
        with tempfile.TemporaryDirectory() as temp:
            exe=Path(temp)/'trust-record.exe'
            subprocess.run([str(root/'.tools/go/bin/go.exe'),'build','-o',str(exe),'./cmd/architecture-trust-record'],cwd=root,check=True,capture_output=True,timeout=60)
            raw=json.dumps(head(),ensure_ascii=False).encode()
            run=subprocess.run([str(exe)],input=raw,capture_output=True,timeout=10)
            self.assertEqual(run.returncode,0,run.stderr);result=json.loads(run.stdout)
            validate_record(result,'domain-1');self.assertEqual(canonical(raw),canonical(run.stdout))
            for mutate in (lambda h:h.update(high_water_version=1),lambda h:h.update(row_version=0),lambda h:h.update(provider_url='https://invalid.example'),lambda h:h['active'].update(digest='bad')):
                h=head();mutate(h)
                run=subprocess.run([str(exe)],input=json.dumps(h).encode(),capture_output=True,timeout=10)
                self.assertNotEqual(run.returncode,0)


if __name__=='__main__':unittest.main()

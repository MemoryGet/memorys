"""Synthetic verifier claims only; does not prove signature or live authorization."""
import copy
import json
from pathlib import Path
import unittest
from jsonschema import ValidationError
from access_grant_validation import BINDINGS,binding_digest,check_bindings
from build_access_grant import build


class AccessGrantTests(unittest.TestCase):
    def setUp(self):
        self.claims={'format':'akos.grant-bindings/1','issuer_id':'issuer-1','audience_module_id':'module-1',
                     'caller_service_id':'service-1','subject_id':'user-1','action':'blob.read','method':'POST',
                     'path':'/modules/v1/blob_store/open','invocation_id':'invoke-1','request_digest':'a'*64,
                     'scope_digest':'b'*64,'profile_digest':'c'*64,'issued_at_ns':100,'not_before_ns':100,'expires_at_ns':200}
        self.expected={k:self.claims[k] for k in BINDINGS}

    def check(self,claims=None,now=150):
        return check_bindings(claims or self.claims,self.expected,now,200,250,100)

    def test_shape_and_valid_bindings_are_not_authorization(self):
        saved=json.loads((Path(__file__).parent/'schemas/access-grant-bindings.v1.schema.json').read_text(encoding='utf-8'))
        self.assertEqual(saved,build());self.assertFalse(self.check()['authorized'])

    def test_every_identity_action_object_digest_binding_rejects_change(self):
        for key in BINDINGS:
            c=copy.deepcopy(self.claims)
            c[key]='d'*64 if key.endswith('digest') else ('DELETE' if key=='method' else '/modules/v1/blob_store/delete' if key=='path' else 'different')
            with self.subTest(key=key),self.assertRaisesRegex(ValueError,'binding_mismatch'):self.check(c)

    def test_expiry_future_issue_long_lifetime_and_scope_boundary(self):
        with self.assertRaisesRegex(ValueError,'time_window'):self.check(now=200)
        c=copy.deepcopy(self.claims);c.update(issued_at_ns=160,not_before_ns=160)
        with self.assertRaisesRegex(ValueError,'time_window'):self.check(c)
        c=copy.deepcopy(self.claims);c['expires_at_ns']=201
        with self.assertRaisesRegex(ValueError,'lifetime'):self.check(c)
        with self.assertRaisesRegex(ValueError,'time_window'):check_bindings(self.claims,self.expected,150,200,199,100)

    def test_exact_nanoseconds_and_unknown_provider_claims(self):
        c=copy.deepcopy(self.claims);c['issued_at_ns']=100.0
        with self.assertRaisesRegex(ValueError,'exact_time'):self.check(c)
        c=copy.deepcopy(self.claims);c['signature']='not-verified'
        with self.assertRaises(ValidationError):self.check(c)

    def test_canonical_bytes_and_domain_bind_unchanged_request(self):
        a=b'{"scope":{"n":9007199254740993},"input":"text"}'
        b=b'{ "input": "text", "scope": {"n":9007199254740993} }'
        self.assertEqual(binding_digest(a,'request'),binding_digest(b,'request'))
        self.assertNotEqual(binding_digest(a,'request'),binding_digest(a,'scope'))
        self.assertNotEqual(binding_digest(a,'request'),binding_digest(a.replace(b'993',b'992'),'request'))
        with self.assertRaises(ValueError):binding_digest(b'{"input":1,"input":2}','request')


if __name__=='__main__':unittest.main()

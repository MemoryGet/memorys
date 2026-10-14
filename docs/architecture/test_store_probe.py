import copy
import json
import unittest
from test_store_binding import fixture as store_fixture
from store_probe_validation import load_probe,validate_probe

NOW=1900000000000000123
def fixture():
    old=store_fixture();e=old['expectation'];b={k:k for k in ('check_id','probe_id','invocation_id','request_id','control_domain_id','scope_id','subject_id')}
    b.update(binding_id=e['binding_id'],binding_version=e['identity']['binding_version'],policy_version=9007199254740993,
             expected_binding_digest='b'*64,request_profile_digest='c'*64,endpoint=e['endpoints'][0],issued_at_ns=NOW-1000000000,deadline_ns=NOW+1000000000)
    r={'request':{'request_id':b['request_id'],'profile_digest':b['request_profile_digest'],'deadline_ns':b['deadline_ns']},
       'scope':{'id':b['scope_id'],'subject_id':b['subject_id'],'domain_id':b['control_domain_id'],'action':'metadata.store.observe','policy_version':b['policy_version'],'expires_at_ns':b['deadline_ns']},'binding':b}
    return {'schema_version':1,'expectation':e,'request':r,'observation':{'binding':copy.deepcopy(b),'observation':old['observations'][0],'observed_at_ns':NOW}}

class ProbeTests(unittest.TestCase):
    def reject(self,change):
        b=fixture();change(b)
        with self.assertRaises(ValueError):validate_probe(b,NOW)

    def test_exact_versions_and_explicit_unverified_result(self):
        b=fixture();self.assertEqual(load_probe(json.dumps(b).encode(),NOW),b)
        report=validate_probe(b,NOW);self.assertTrue(report['binding_checked'])
        for k in ('authenticated','coverage_verified','ready_for_service'):self.assertFalse(report[k])

    def test_old_probe_and_mutated_expected_endpoint(self):
        self.reject(lambda b:b['observation']['binding'].update(probe_id='old'))
        self.reject(lambda b:b['observation']['binding'].update(expected_binding_digest='d'*64))
        self.reject(lambda b:b['observation']['observation']['endpoint'].update(endpoint_identity='other'))

    def test_scope_request_and_store_mutation(self):
        self.reject(lambda b:b['request']['scope'].update(action='knowledge.search'))
        self.reject(lambda b:b['request']['scope'].update(domain_id='other'))
        self.reject(lambda b:b['request']['request'].update(deadline_ns=NOW+1))
        self.reject(lambda b:b['observation']['observation']['identity'].update(metadata_store_epoch_id='backup'))

    def test_nanosecond_temporal_boundaries(self):
        b=fixture()
        with self.assertRaises(ValueError):validate_probe(b,b['request']['binding']['deadline_ns'])
        self.reject(lambda b:b['observation'].update(observed_at_ns=b['request']['binding']['issued_at_ns']-1))
        self.reject(lambda b:b['observation'].update(observed_at_ns=NOW+1))

    def test_closed_fields_raw_duplicates_and_exact_integer_clock(self):
        self.reject(lambda b:b.update(extra=1))
        self.reject(lambda b:b['request']['binding'].update(policy_version=1.0))
        raw=json.dumps(fixture()).encode().replace(b'"schema_version": 1',b'"schema_version": 1, "schema_version": 1')
        with self.assertRaises(ValueError):load_probe(raw,NOW)
        with self.assertRaises(ValueError):validate_probe(fixture(),float(NOW))

if __name__=='__main__':unittest.main()

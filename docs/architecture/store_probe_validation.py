"""Supplied bound-probe arithmetic; no trusted clock, identity or replay ledger."""
import json
from jsonschema import Draft202012Validator
from build_store_probe import build


def validate_probe(bundle, now_ns):
    try:
        Draft202012Validator(build()).validate(bundle)
        if type(now_ns) is not int or not 0 < now_ns <= 9223372036854775807: raise ValueError()
        def exact(value):
            if isinstance(value, str):
                if len(value.encode('utf-8',errors='strict'))>4096:raise ValueError()
            elif isinstance(value, dict):
                for k,v in value.items():exact(k);exact(v)
            elif isinstance(value,list):
                for v in value:exact(v)
            elif type(value) is not int:raise ValueError()
        exact(bundle)
        e,r,a=bundle['expectation'],bundle['request'],bundle['observation']
        b=r['binding'];s=r['scope'];context=r['request']
        endpoints={}
        for endpoint in e['endpoints']:
            if endpoint['role'] in endpoints:raise ValueError()
            endpoints[endpoint['role']]=endpoint
        if not {'metadata_uow','query_read_unit'}.issubset(endpoints):raise ValueError()
        if b['binding_id']!=e['binding_id'] or b['binding_version']!=e['identity']['binding_version'] or endpoints.get(b['endpoint']['role'])!=b['endpoint']:raise ValueError()
        if any(context[k]!=b[v] for k,v in [('request_id','request_id'),('profile_digest','request_profile_digest'),('deadline_ns','deadline_ns')]):raise ValueError()
        if any(s[k]!=b[v] for k,v in [('id','scope_id'),('subject_id','subject_id'),('domain_id','control_domain_id'),('policy_version','policy_version')]) or s['expires_at_ns']<b['deadline_ns']:raise ValueError()
        if a['binding']!=b or not b['issued_at_ns']<=a['observed_at_ns']<=now_ns<b['deadline_ns']:raise ValueError()
        o=a['observation']
        if o['binding_id']!=e['binding_id'] or o['identity']!=e['identity'] or o['endpoint']!=b['endpoint']:raise ValueError()
        return {'binding_checked':True,'authenticated':False,'coverage_verified':False,'ready_for_service':False}
    except Exception:
        raise ValueError('invalid_store_probe_review') from None


def load_probe(raw,now_ns):
    if not isinstance(raw,bytes) or len(raw)>4<<20:raise ValueError('invalid_store_probe_review')
    def pairs(items):
        result={}
        for k,v in items:
            if k in result:raise ValueError()
            result[k]=v
        return result
    def reject(_):raise ValueError()
    try:
        b=json.loads(raw.decode('utf-8',errors='strict'),object_pairs_hook=pairs,parse_float=reject,parse_constant=reject)
        validate_probe(b,now_ns)
        return b
    except Exception:
        raise ValueError('invalid_store_probe_review') from None

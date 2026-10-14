"""Offline binding arithmetic only, does not verify tokens or authorize calls."""
import hashlib
from jsonschema import Draft202012Validator
from canonical_content import canonical
from build_access_grant import build

BINDINGS=('issuer_id','audience_module_id','caller_service_id','subject_id','action','method','path',
          'invocation_id','request_digest','scope_digest','profile_digest')
TIMES=('issued_at_ns','not_before_ns','expires_at_ns')


def binding_digest(raw_json, kind):
    if kind not in {'request','scope'}:raise ValueError('grant_digest_kind_invalid')
    if not isinstance(raw_json,bytes):raise ValueError('grant_raw_bytes_required')
    encoded=canonical(raw_json)
    if not encoded.startswith(b'{'):raise ValueError('grant_object_required')
    return hashlib.sha256(('akos.grant-'+kind+'/1').encode()+b'\x00'+encoded).hexdigest()


def check_bindings(claims, expected, now_ns, request_deadline_ns, scope_expires_ns, max_lifetime_ns):
    Draft202012Validator(build()).validate(claims)
    if not isinstance(expected,dict) or set(expected)!=set(BINDINGS):raise ValueError('grant_expected_bindings_required')
    for value in [now_ns,request_deadline_ns,scope_expires_ns,max_lifetime_ns]+[claims[k] for k in TIMES]:
        if type(value) is not int or not 0<=value<=9223372036854775807:raise ValueError('grant_exact_time_required')
    if any(claims[k]!=expected[k] for k in BINDINGS):raise ValueError('grant_binding_mismatch')
    issued,nbf,exp=(claims[k] for k in TIMES)
    # No default skew: deployment must use authoritative clocks and explicit bounded policy.
    if not issued<=nbf<=now_ns<exp or not now_ns<request_deadline_ns<=scope_expires_ns:
        raise ValueError('grant_time_window_invalid')
    if not exp<=min(request_deadline_ns,scope_expires_ns) or max_lifetime_ns<=0 or exp-issued>max_lifetime_ns:
        raise ValueError('grant_lifetime_invalid')
    return {'bindings_checked':True,'authorized':False,'unverified':['token_signature_and_algorithm','issuer_and_key_trust',
            'authenticated_service_identity','current_permission_and_publication','persistent_invocation_ledger','streamed_content_integrity']}

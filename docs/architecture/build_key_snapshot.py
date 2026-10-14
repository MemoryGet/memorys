"""Closed public-key snapshot Schema and independent digest; no issuer trust."""
import argparse
import base64
import copy
import hashlib
import json
from pathlib import Path
from canonical_content import canonical
from jsonschema import Draft202012Validator


def build():
    ident={'type':'string','minLength':1,'maxLength':200}
    ns={'type':'integer','minimum':0,'maximum':9223372036854775807}
    def obj(props):return {'type':'object','additionalProperties':False,'required':list(props),'properties':props}
    key=obj({'key_id':ident,'algorithm':{'const':'EdDSA'},'public_key':{'type':'string','pattern':'^[A-Za-z0-9+/]{43}=$'},
             'enabled_at_ns':ns,'sign_until_ns':ns,'verify_until_ns':ns})
    return {'$schema':'https://json-schema.org/draft/2020-12/schema','title':'AI Knowledge OS public key snapshot V1',
            **obj({'snapshot_id':ident,'version':{'type':'integer','minimum':1,'maximum':18446744073709551615},'issuer_id':ident,
                   'valid_from_ns':ns,'valid_until_ns':ns,'max_lifetime_ns':{'type':'integer','minimum':1,'maximum':9223372036854775807},
                   'keys':{'type':'array','minItems':1,'maxItems':1000,'items':key}})}


def inspect_snapshot(raw):
    canonical(raw)  # Reject duplicate keys/invalid Unicode/depth before ordinary decoding.
    value=json.loads(raw)
    Draft202012Validator(build()).validate(value)
    def exact(v):
        if isinstance(v,dict):
            for key,item in v.items():
                if key=='version' or key.endswith('_ns'):
                    if type(item) is not int:raise ValueError('key_snapshot_integer_required')
                exact(item)
        elif isinstance(v,list):
            for child in v:exact(child)
    exact(value)
    if value['valid_from_ns']>=value['valid_until_ns']:raise ValueError('key_snapshot_window_invalid')
    ids=set()
    for key in value['keys']:
        if key['key_id'] in ids:raise ValueError('key_snapshot_duplicate_kid')
        ids.add(key['key_id'])
        pub=base64.b64decode(key['public_key'],validate=True)
        if len(pub)!=32 or base64.b64encode(pub).decode()!=key['public_key']:raise ValueError('key_snapshot_base64_invalid')
        if not key['enabled_at_ns']<key['sign_until_ns']<=key['verify_until_ns']:raise ValueError('key_snapshot_window_invalid')
    for ident in [value['snapshot_id'],value['issuer_id']]+list(ids):
        if len(ident.encode('utf-8'))>200:raise ValueError('key_snapshot_identity_budget')
    normalized=copy.deepcopy(value);normalized['keys'].sort(key=lambda k:k['key_id'])
    data=json.dumps(normalized,ensure_ascii=False,separators=(',',':')).encode()
    digest=hashlib.sha256(b'akos.key-snapshot/1\x00'+canonical(data)).hexdigest()
    return {'id':value['snapshot_id'],'version':value['version'],'digest':digest}


def golden():
    key={'key_id':'key-1','algorithm':'EdDSA','public_key':base64.b64encode(bytes(range(32))).decode(),
         'enabled_at_ns':0,'sign_until_ns':9007199254740993,'verify_until_ns':9007199254740994}
    config={'snapshot_id':'trust-1','version':9007199254740993,'issuer_id':'issuer-1','valid_from_ns':0,
            'valid_until_ns':9223372036854775807,'max_lifetime_ns':1000000000,'keys':[key]}
    raw=json.dumps(config,separators=(',',':'))
    return {'format':'akos.key-snapshot-golden/1','scope':'Synthetic public bytes for configuration hashing, not signed credentials',
            'config_json':raw,'expected':inspect_snapshot(raw.encode())}


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--write',action='store_true');args=p.parse_args()
    root=Path(__file__).parent
    for name,value in [('schemas/key-snapshot.v1.schema.json',build()),('examples/key-snapshot-golden.v1.json',golden())]:
        text=json.dumps(value,ensure_ascii=False,indent=2)+'\n';path=root/name
        if args.write:path.write_text(text,encoding='utf-8')
        elif path.read_text(encoding='utf-8')!=text:raise SystemExit(name+' is stale')
    if not args.write:print('PASS: key snapshot Schema and Python canonical identity golden')

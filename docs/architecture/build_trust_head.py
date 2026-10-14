"""One typed metadata family, not the complete import contract."""
import argparse,json
from pathlib import Path
from jsonschema import Draft202012Validator


def build():
    ident={'type':'string','minLength':1,'maxLength':200};digest={'type':'string','pattern':'^[a-f0-9]{64}$'}
    version={'type':'integer','minimum':1,'maximum':18446744073709551615}
    def obj(props):return {'type':'object','additionalProperties':False,'required':list(props),'properties':props}
    return {'$schema':'https://json-schema.org/draft/2020-12/schema','title':'akos.metadata.trust_head/1',
            **obj({'domain_id':ident,'issuer_id':ident,'active':obj({'id':ident,'version':version,'digest':digest}),
                   'build_lock_digest':digest,'high_water_version':version,'row_version':version})}


def validate_record(payload,record_id):
    Draft202012Validator(build()).validate(payload)
    for value in (payload['active']['version'],payload['high_water_version'],payload['row_version']):
        if type(value) is not int:raise ValueError('trust_head_integer_required')
    if payload['high_water_version']!=payload['active']['version']:raise ValueError('trust_head_high_water_mismatch')
    if record_id!=payload['domain_id']:raise ValueError('trust_head_record_id_mismatch')


def validate_advance(before,after):
    validate_record(before,before['domain_id']);validate_record(after,before['domain_id'])
    if before['issuer_id']!=after['issuer_id']:raise ValueError('trust_head_issuer_changed')
    if after['row_version']!=before['row_version']+1:raise ValueError('trust_head_row_version_advance')
    if after['high_water_version']<=before['high_water_version']:raise ValueError('trust_head_high_water_not_advancing')


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--write',action='store_true');args=p.parse_args()
    path=Path(__file__).parent/'schemas/trust-head.v1.schema.json';text=json.dumps(build(),ensure_ascii=False,indent=2)+'\n'
    if args.write:path.write_text(text,encoding='utf-8')
    elif path.read_text(encoding='utf-8')!=text:raise SystemExit('trust head schema is stale')
    else:print('PASS: typed trust head schema generation consistency')

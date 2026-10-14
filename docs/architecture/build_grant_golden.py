"""Explicitly regenerate reviewed cross-language binding vectors; not verified grants."""
import argparse
import json
from pathlib import Path
from access_grant_validation import binding_digest
from canonical_content import canonical
from check_parser_wire_mapping import fixture_cases


def build():
    request=fixture_cases()[0]['request']
    cases=[('typed_parser_request','request',json.dumps(request,ensure_ascii=False,separators=(',',':'))),
           ('typed_scope_large_policy','scope',json.dumps(request['context']['scope'],ensure_ascii=False)),
           ('unicode_lf','request','{"input":"中文😀\\n原文  ","n":9007199254740993}'),
           ('unicode_crlf','request','{"input":"中文😀\\r\\n原文  ","n":9007199254740993}'),
           ('exact_decimal','scope','{"n":0.0000000100,"a":[1,2]}'),
           ('key_order','scope','{ "a": [1.00, 2e0], "n": 1e-8 }')]
    return {'format':'akos.grant-golden/1','encoding':'AKOS-CJSON/1',
            'cases':[{'name':name,'kind':kind,'input_json':raw,'canonical_json':canonical(raw.encode()).decode(),
                      'digest':binding_digest(raw.encode(),kind)} for name,kind,raw in cases]}


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--write',action='store_true');args=p.parse_args()
    path=Path(__file__).parent/'examples/grant-binding-golden.v1.json';text=json.dumps(build(),ensure_ascii=False,indent=2)+'\n'
    if args.write:path.write_text(text,encoding='utf-8')
    elif path.read_text(encoding='utf-8')!=text:raise SystemExit('grant golden vectors are stale')
    else:print('PASS: six Python grant binding vectors and canonical bytes')

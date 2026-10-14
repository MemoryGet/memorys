"""All-operation canonical bindings; fixture scopes are not real authorizations."""
import argparse
import copy
import json
from pathlib import Path
from access_grant_validation import binding_digest
from canonical_content import canonical
from build_remote_contract import build as remote_spec
from remote_wire_validation import validate
from check_parser_wire_mapping import fixture_cases
from check_retrieval_wire_mapping import fixtures as retrieval
from check_index_wire_mapping import fixtures as indexing
from check_blob_wire_mapping import fixtures as blobs
from check_generation_wire_mapping import fixtures as generation
from check_control_wire_mapping import fixtures as control
from remote_operation_policy import policy,check_operation


def build():
    examples=[dict(fixture_cases()[0],operation='parser_parse')]+retrieval()+indexing()+blobs()+generation()+control()
    composite=copy.deepcopy(retrieval()[1]);composite['operation']='composite_ingest_search_search'
    composite['request']['context']['module_instance_id']='composite-a';examples.append(composite)
    selected={}
    for example in examples:selected.setdefault(example['operation'],example['request'])
    spec=remote_spec()
    operations={v['post']['operationId']:(p,'POST') for p,v in spec['paths'].items()}
    if set(selected)!=set(operations):raise AssertionError('Incomplete grant operation inventory')
    cases=[]
    policies=policy()
    for name in sorted(selected):
        request=copy.deepcopy(selected[name])
        request['context']['scope']['action']=policies[name]['allowed_actions'][0]
        validate(spec,name+'_Request',request)
        raw=json.dumps(request,ensure_ascii=False,separators=(',',':'))
        scope=json.dumps(request['context']['scope'],ensure_ascii=False,separators=(',',':'))
        changed=copy.deepcopy(request)
        # Deliberately invalid input: hashing must bind even content that later schema validation rejects.
        changed['input']={'mutation_probe':name}
        changed_raw=json.dumps(changed,ensure_ascii=False,separators=(',',':'))
        path,method=operations[name]
        check_operation(name,method,path,request['context']['scope'])
        cases.append({'operation':name,'method':method,'path':path,'request_json':raw,'scope_json':scope,
                      'canonical_request':canonical(raw.encode()).decode(),'request_digest':binding_digest(raw.encode(),'request'),
                      'scope_digest':binding_digest(scope.encode(),'scope'),'mutated_request_json':changed_raw,
                      'mutated_request_digest':binding_digest(changed_raw.encode(),'request')})
    return {'format':'akos.operation-grant-golden/1','scope':'Schema-shaped requests; no real operation permissions or verified Grant', 'cases':cases}


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--write',action='store_true');args=p.parse_args()
    path=Path(__file__).parent/'examples/operation-grant-golden.v1.json';text=json.dumps(build(),ensure_ascii=False,indent=2)+'\n'
    if args.write:path.write_text(text,encoding='utf-8')
    elif path.read_text(encoding='utf-8')!=text:raise SystemExit('operation grant vectors are stale')
    else:print('PASS: 17 independently Schema-checked request/scope golden bindings and input mutation digests')

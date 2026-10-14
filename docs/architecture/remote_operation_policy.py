"""Offline operation/action allow-list; never grants permissions or validates a job."""
from build_remote_contract import build

RULES={
 'parser_parse':(['document.parse'],'job'),
 'blob_store_put':(['blob.put'],'job'),
 'blob_store_open':(['blob.open'],'read'),
 'blob_store_delete':(['blob.delete'],'job'),
 'embedder_embed':(['knowledge.search','knowledge.ask','index.prepare'],'pipeline'),
 'retriever_search':(['knowledge.search','knowledge.ask'],'read'),
 'reranker_rerank':(['knowledge.search','knowledge.ask'],'pipeline'),
 'generator_generate':(['knowledge.ask'],'pipeline'),
 'index_writer_prepare':(['index.prepare'],'job'),
 'index_writer_verify':(['index.verify'],'job'),
 'index_writer_delete':(['index.delete'],'job'),
 'composite_ingest_search_prepare_projection':(['index.prepare'],'job'),
 'composite_ingest_search_verify':(['index.verify'],'job'),
 'composite_ingest_search_delete':(['index.delete'],'job'),
 'composite_ingest_search_search':(['knowledge.search','knowledge.ask'],'read'),
 'control_status':(['invocation.status'],'control'),
 'control_cancel':(['invocation.cancel'],'control')}


def policy():
    spec=build(); routes={v['post']['operationId']:p for p,v in spec['paths'].items()}
    if set(routes)!=set(RULES):raise ValueError('operation_policy_inventory_mismatch')
    return {name:{'method':'POST','path':routes[name],'allowed_actions':list(actions),'execution_class':kind}
            for name,(actions,kind) in sorted(RULES.items())}


def check_operation(operation,method,path,scope):
    rules=policy()
    if operation not in rules:raise ValueError('operation_policy_unknown')
    rule=rules[operation]
    if method!=rule['method'] or path!=rule['path']:raise ValueError('operation_policy_route_mismatch')
    if not isinstance(scope,dict) or scope.get('action') not in rule['allowed_actions']:
        raise ValueError('operation_policy_action_mismatch')
    if not isinstance(scope.get('kb_ids'),list) or not scope['kb_ids']:
        raise ValueError('operation_policy_bounded_scope_required')
    if any(not isinstance(v,str) or not v for v in scope['kb_ids']) or not isinstance(scope.get('space_id'),str) or not scope['space_id']:
        raise ValueError('operation_policy_bounded_scope_required')
    if len(set(scope['kb_ids']))!=len(scope['kb_ids']):raise ValueError('operation_policy_bounded_scope_required')
    return {'shape_checked':True,'execution_class':rule['execution_class'],'authorized':False,
            'unverified':['issuer_and_service_identity','current_subject_permissions','trusted_pipeline_or_job',
                          'resource_ownership_and_publication','lease_and_holds','control_target_ownership']}


if __name__=='__main__':
    import argparse,json
    from pathlib import Path
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--write',action='store_true');args=parser.parse_args()
    path=Path(__file__).parent/'examples/remote-operation-policy.v1.json'
    text=json.dumps({'format':'akos.operation-policy/1','operations':policy()},ensure_ascii=False,indent=2)+'\n'
    if args.write:path.write_text(text,encoding='utf-8')
    elif path.read_text(encoding='utf-8')!=text:raise SystemExit('operation policy is stale')
    else:print('PASS: 17 operation route/action policy generation consistency')

"""Independently validate actual authorized historical REST/MCP read evidence."""
import base64
import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator
from canonical_content import canonical, frozen
from check_architecture import source_inventory
from check_frozen_content import validate_payload

ROOT = Path(__file__).resolve().parents[2]


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or not report.get('sources_unchanged_during_checks'):
        raise ValueError('stale or failed historical read evidence')
    proof = report['go_repository_proof']
    samples = proof.get('historical_read_samples', [])
    if len(samples) != 1:
        raise ValueError('missing actual historical read sample')
    spec = json.loads((ROOT / 'docs/architecture/openapi/partner.v1.json').read_text(encoding='utf-8'))
    validators = {name: Draft202012Validator({'$ref': '#/components/schemas/' + name, 'components': spec['components']}) for name in ('ReadRequest', 'ReadResult', 'Block', 'Citation')}
    for sample in samples:
        request, continuation = sample['request'], sample['continuation']
        validators['ReadRequest'].validate(request)
        validators['ReadRequest'].validate(continuation)
        source, query = sample['source_record'], sample['source_request']
        record, review, current = source['Content'], source['Review'], source['Current']
        doc, revision = request['document_id'], request['revision_id']
        if continuation['document_id'] != doc or continuation['revision_id'] != revision or query['Base']['DocumentID'] != doc or query['Base']['RevisionID'] != revision:
            raise ValueError('historical target/request binding differs')
        base, history, principal = query['Base']['Scope'], query['HistoryScope'], query['Principal']
        if base['Action'] != 'knowledge.read' or history['Action'] != 'knowledge.history.read' or base['SubjectID'] != principal['SubjectID'] or any(base[k] != history[k] for k in ('SubjectID', 'SpaceID', 'KnowledgeBaseIDs', 'PolicyVersion')) or len(base['KnowledgeBaseIDs']) != 1:
            raise ValueError('distinct historical scope intersection missing')
        grants = sample['stored_grants']
        if len(grants) != 2 or {g['action'] for g in grants} != {'knowledge.read', 'knowledge.history.read'}:
            raise ValueError('actual independent ACL rows missing')
        for grant in grants:
            if (grant['space_id'], grant['kb_id'], grant['issuer'], grant['subject_id'], grant['client_id'], int(grant['version'])) != (base['SpaceID'], base['KnowledgeBaseIDs'][0], principal['Issuer'], principal['SubjectID'], principal['ClientID'], base['PolicyVersion']):
                raise ValueError('stored historical ACL identity differs')
        old, new = sample['old_publication'], sample['new_publication']
        if old['Action'] != 'publish' or new['Action'] != 'publish' or old['DocumentID'] != doc or new['DocumentID'] != doc or old['RevisionID'] != revision or new['RevisionID'] != sample['replaced_by'] or new['PreviousRevisionID'] != revision or old['PublicationEpoch'] >= new['PublicationEpoch'] or source['PublicationID'] != old['ID'] or source['PublicationEpoch'] != old['PublicationEpoch'] or current['RevisionID'] != new['RevisionID'] or current['PublicationEpoch'] != new['PublicationEpoch'] or not source['StoreEpochID']:
            raise ValueError('actual prior publication/replacement differs')
        payload = base64.b64decode(record['Payload'], validate=True)
        if payload != canonical(payload) or record['DocumentID'] != doc or record['RevisionID'] != revision:
            raise ValueError('historical record identity/canonical bytes differ')
        content = json.loads(payload)
        validate_payload(content)
        hashes = frozen(payload)
        fact, approval, validity = review['Revision'], review['Approval'], review['Validity']
        if fact['ID'] != revision or fact['DocumentID'] != doc or hashes['content_hash'] != fact['ContentHash'] or hashes['blocks_hash'] != fact['BlocksHash'] or fact['HashFormat'] != hashes['format'] or approval['RevisionID'] != revision or approval['ContentHash'] != fact['ContentHash'] or approval['Decision'] != 'approve' or validity['ApprovalID'] != approval['ID'] or validity['ApprovalRowVersion'] != approval['RowVersion'] or validity['State'] != 'effective' or validity['ReviewPolicyVersion'] <= 0:
            raise ValueError('historical frozen approval commitment differs')
        deletion = sample['old_index_cleanup']
        if deletion['GenerationID'] != sample['old_generation_id'] or deletion['DocumentID'] != doc or deletion['RevisionID'] != revision or deletion['Deleted'] is not True or deletion['RecordCount'] != 0:
            raise ValueError('actual old-index deletion evidence missing')
        originals = {}
        for original in proof['evidence_backend_samples']:
            blob = original['original_blob']
            data = base64.b64decode(original['original_bytes'], validate=True)
            if hashlib.sha256(data).hexdigest() != blob['SHA256'] or len(data) != blob['Size']:
                raise ValueError('actual original bytes differ')
            originals[blob['ID']] = data.decode('utf-8')
        blocks, citations = [], []
        for block in content['blocks']:
            anchor = block['source_anchor']
            if block['kind'] != 'text' or anchor['kind'] != 'original' or anchor['locator'] != {'precision': 'coarse'} or not block['text'] or block['text'] not in originals[anchor['blob_id']]:
                raise ValueError('historical entire block lacks original support')
            blocks.append({**{k: v for k, v in block.items() if k != 'order'}, 'revision_id': revision})
            citations.append({'document_id': doc, 'revision_id': revision, 'block_id': block['id'], 'source_anchor': anchor})
        if len(blocks) != 2:
            raise ValueError('historical paging fixture missing')
        rest = sample['rest_result']
        mcp = sample['mcp_result']['result']
        second = mcp['structuredContent']
        validators['ReadResult'].validate(rest)
        validators['ReadResult'].validate(second)
        if mcp.get('isError', False) or len(mcp['content']) != 1 or json.loads(mcp['content'][0]['text']) != second:
            raise ValueError('historical MCP representations differ')
        for index, page in enumerate((rest, second)):
            if page['document_id'] != doc or page['revision_id'] != revision or page['blocks'] != [blocks[index]] or page['citations'] != [citations[index]] or page['profile_digest'] != query['Base']['Request']['ProfileDigest']:
                raise ValueError('historical page changed frozen target or fields')
        if continuation['cursor'] != rest.get('next_cursor') or 'next_cursor' in second or rest['request_id'] != query['Base']['Request']['RequestID']:
            raise ValueError('actual cross-transport continuation differs')
        # A positive read by the second actor distinguishes cursor isolation
        # from a denial caused merely by missing identity or source permission.
        second_principal = sample['second_principal']
        second_query = sample['second_source_request']
        second_base, second_history = second_query['Base']['Scope'], second_query['HistoryScope']
        if second_principal['SubjectID'] == principal['SubjectID'] or second_query['Principal'] != second_principal:
            raise ValueError('cross-subject fixture has no independent authenticated actor')
        for actor_scope, action in ((second_base, 'knowledge.read'), (second_history, 'knowledge.history.read')):
            if actor_scope['Action'] != action or actor_scope['SubjectID'] != second_principal['SubjectID'] or any(actor_scope[k] != base[k] for k in ('SpaceID', 'KnowledgeBaseIDs', 'PolicyVersion')):
                raise ValueError('second subject scope/authorization differs')
        if second_query['Base']['DocumentID'] != doc or second_query['Base']['RevisionID'] != revision:
            raise ValueError('second subject read a different historical target')
        second_grants = sample['second_stored_grants']
        if len(second_grants) != 2 or {g['action'] for g in second_grants} != {'knowledge.read', 'knowledge.history.read'}:
            raise ValueError('second subject independent ACL rows missing')
        for grant in second_grants:
            if (grant['space_id'], grant['kb_id'], grant['issuer'], grant['subject_id'], grant['client_id'], int(grant['version'])) != (base['SpaceID'], base['KnowledgeBaseIDs'][0], second_principal['Issuer'], second_principal['SubjectID'], second_principal['ClientID'], base['PolicyVersion']):
                raise ValueError('second subject stored ACL binding differs')
        token = sample['second_token_status']
        if token['revoked'] or (token['issuer'], token['subject_id'], token['client_id']) != (second_principal['Issuer'], second_principal['SubjectID'], second_principal['ClientID']) or not token['token_id'] or not token['key_id']:
            raise ValueError('second subject registered token status differs')
        second_rest = sample['second_rest_result']
        validators['ReadResult'].validate(second_rest)
        for key in ('document_id', 'revision_id', 'blocks', 'citations', 'profile_digest'):
            if second_rest[key] != rest[key]:
                raise ValueError('second subject did not independently read identical first page')
        if not second_rest.get('next_cursor') or second_rest['next_cursor'] == rest['next_cursor'] or sample.get('cross_subject_cursor_rejected') is not True:
            raise ValueError('cross-subject cursor rejection evidence missing')
        unpublished, revoked = sample['unpublished_approval'], sample['revoked_approval']
        if unpublished['RevisionID'] != sample['unpublished_revision'] or unpublished['Decision'] != 'approve' or sample['unpublished_publication_count'] != 0 or unpublished['RevisionID'] in (revision, current['RevisionID']):
            raise ValueError('approved-but-unpublished fixture differs')
        if revoked['RevisionID'] != revision or revoked['Decision'] != 'revoke' or revoked['ContentHash'] != approval['ContentHash'] or revoked['RowVersion'] != approval['RowVersion'] + 1:
            raise ValueError('actual historical approval revocation missing')
        if not all(sample.get(k) is True for k in ('unpublished_target_rejected', 'revoked_target_rejected', 'changed_target_cursor_rejected', 'missing_target_rejected')) or sample['authority'] != 'actual-rs256-postgres-read-and-history' or sample['original_grant'] != 'fixture-independent-read':
            raise ValueError('historical denial/authority boundary differs')
    lifecycle = proof.get('historical_lifecycle_samples', [])
    if len(lifecycle) != 1:
        raise ValueError('missing actual historical lifecycle sample')
    item = lifecycle[0]
    before, after, query = item['before'], item['after'], item['request']
    history = item['history']
    if len(history) != 8 or [int(h['publication_epoch']) for h in history] != list(range(1, 9)) or [h['action'] for h in history] != ['publish'] * 4 + ['withdraw'] + ['publish'] * 3:
        raise ValueError('actual lifecycle sequence differs')
    doc, target = query['Base']['DocumentID'], query['Base']['RevisionID']
    other = history[2]['revision_id']
    if target == other or any(h['document_id'] != doc or int(h['previous_epoch']) != i for i, h in enumerate(history)):
        raise ValueError('lifecycle document/epoch continuity differs')
    expected_revisions = [history[0]['revision_id'], target, other, target, None, other, target, other]
    if [h['revision_id'] for h in history] != expected_revisions or any(history[i]['previous_revision_id'] != expected_revisions[i-1] for i in range(1, 8)):
        raise ValueError('withdraw/republish target relationship differs')
    withdrawn = item['withdrawal']
    if withdrawn['HistoryID'] != history[4]['id'] or withdrawn['DocumentID'] != doc or withdrawn['PublicationEpoch'] != 5:
        raise ValueError('withdrawal receipt differs from actual history')
    for record, target_index, current_index in ((before, 1, 2), (after, 6, 7)):
        content = record['Content']
        raw = base64.b64decode(content['Payload'], validate=True)
        validate_payload(json.loads(raw))
        hashes = frozen(raw)
        review = record['Review']
        if raw != canonical(raw) or content['DocumentID'] != doc or content['RevisionID'] != target or record['PublicationID'] != history[target_index]['id'] or record['PublicationEpoch'] != target_index + 1 or record['Current']['RevisionID'] != other or record['Current']['PublicationEpoch'] != current_index + 1 or review['Revision']['ContentHash'] != hashes['content_hash'] or review['Revision']['BlocksHash'] != hashes['blocks_hash'] or review['Approval']['RevisionID'] != target or review['Approval']['Decision'] != 'approve' or review['Validity']['State'] != 'effective':
            raise ValueError('lifecycle frozen publication/approval differs')
    if before['Content'] != after['Content'] or before['Review'] != after['Review'] or query['Base']['Scope']['PolicyVersion'] != 3 or query['HistoryScope']['PolicyVersion'] != 3:
        raise ValueError('republication changed content/approval or fresh reader policy missing')
    grants = item['stored_grants']
    if len(grants) != 2 or {g['action'] for g in grants} != {'knowledge.read', 'knowledge.history.read'} or any(int(g['version']) != 3 or g['subject_id'] != query['Principal']['SubjectID'] or g['issuer'] != query['Principal']['Issuer'] or g['client_id'] != query['Principal']['ClientID'] or g['space_id'] != query['Base']['Scope']['SpaceID'] or g['kb_id'] != query['Base']['Scope']['KnowledgeBaseIDs'][0] for g in grants):
        raise ValueError('lifecycle native reader grants differ')
    if not item['withdrawn_document_rejected'] or not item['withdrawn_target_after_other_publish_rejected'] or item['governance_authority'] != 'fixture' or item['reader_authority'] != 'postgres-sealed-acl':
        raise ValueError('lifecycle denial/authority evidence missing')
    target_blocks = [b['id'] for b in json.loads(base64.b64decode(before['Content']['Payload']))['blocks']]
    if len(target_blocks) != 2:
        raise ValueError('application lifecycle fixture lacks real two-page content')
    for phase, record in (('before', before), ('after', after)):
        first = item['application_' + phase + '_first']
        last = item['application_' + phase + '_last']
        if first['Record'] != record['Content'] or last['Record'] != record['Content'] or first['BlockIDs'] != target_blocks[:1] or last['BlockIDs'] != target_blocks[1:] or not first['NextCursor'] or last['NextCursor']:
            raise ValueError('actual application lifecycle pages differ from frozen content')
    if item['application_before_first']['NextCursor'] == item['application_after_first']['NextCursor'] or item.get('application_identity') != 'rs256-postgres-status' or not all(item.get(k) is True for k in ('application_withdrawn_rejected', 'application_withdrawn_after_other_publish_rejected', 'application_old_cursor_rejected_after_republish')):
        raise ValueError('application lifecycle old-cursor/withdrawal evidence missing')
    transports = item.get('transport_samples', [])
    if [t['phase'] for t in transports] != ['before', 'withdrawn', 'other-version', 'republished-old-cursor', 'after']:
        raise ValueError('missing actual REST/MCP lifecycle stages')
    frozen_blocks = json.loads(base64.b64decode(before['Content']['Payload']))['blocks']
    old_cursor = transports[0]['rest_result']['next_cursor']
    for transport in transports:
        validators['ReadRequest'].validate(transport['request'])
        if transport['request']['document_id'] != doc or transport['request']['revision_id'] != target or transport['mcp_status'] != 200:
            raise ValueError('transport lifecycle target/status differs')
        result = transport['mcp_result']['result']
        structured = result['structuredContent']
        if len(result['content']) != 1 or json.loads(result['content'][0]['text']) != structured:
            raise ValueError('transport lifecycle MCP representations differ')
        if transport['phase'] in ('before', 'after'):
            if transport['rest_status'] != 200 or result.get('isError', False):
                raise ValueError('transport lifecycle positive read failed')
            rest = transport['rest_result']
            validators['ReadResult'].validate(rest)
            validators['ReadResult'].validate(structured)
            validators['ReadRequest'].validate(transport['continuation'])
            if transport['continuation'] != {**transport['request'], 'cursor': rest['next_cursor']} or 'next_cursor' in structured:
                raise ValueError('transport lifecycle continuation changed')
            for index, page in enumerate((rest, structured)):
                block = frozen_blocks[index]
                expected_block = {**{k: v for k, v in block.items() if k != 'order'}, 'revision_id': target}
                expected_citation = {'document_id': doc, 'revision_id': target, 'block_id': block['id'], 'source_anchor': block['source_anchor']}
                if page['document_id'] != doc or page['revision_id'] != target or page['blocks'] != [expected_block] or page['citations'] != [expected_citation] or page['profile_digest'] != item['application_profile_digest']:
                    raise ValueError('transport lifecycle page changed frozen content/profile')
        else:
            if transport['rest_status'] < 400 or not result.get('isError') or transport['request'].get('cursor') != old_cursor:
                raise ValueError('transport lifecycle old cursor accepted')
            for error in (transport['rest_result'], structured):
                if set(error) != {'code', 'safe_message', 'request_id', 'retryable'} or not error['request_id'] or not error['safe_message'] or not isinstance(error['retryable'], bool):
                    raise ValueError('transport lifecycle denial leaked content or malformed error')
                if transport['phase'] == 'republished-old-cursor' and error['code'] != 'conflict':
                    raise ValueError('republished transport did not reject stale cursor explicitly')
            if transport['phase'] == 'republished-old-cursor' and transport['rest_status'] != 409:
                raise ValueError('republished REST cursor status differs')
    if transports[-1]['rest_result']['next_cursor'] == old_cursor:
        raise ValueError('transport lifecycle revived old cursor')
    if sources != source_inventory():
        raise ValueError('sources changed during historical review')
    return {'passed': True, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': True, 'scope': 'PG16 actual signed-token independently authorized historical REST/MCP continuation, frozen commitments and entire original blocks after old-index deletion; second independently authenticated/authorized reader and cross-subject cursor denial; approved-unpublished/missing/changed cursor/revoked approval denial; governance and original grants remain fixtures; withdraw/other-version publication rejection and explicit target republication recovery through native source port; authenticated application two-page continuation before/after and permanent old-cursor denial; actual REST-to-MCP lifecycle pagination and safe withdrawn/old-cursor transport denials; no delete/complete lifecycle/production/performance acceptance', 'actual_samples': len(samples)}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/historical-read-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 historical read: authority, publication, frozen bytes, original sources and REST/MCP independently verified')

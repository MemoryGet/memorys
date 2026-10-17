"""Validate actual PG16 authenticated read payload and partner content projection."""
import base64
import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator
from canonical_content import canonical, frozen
from check_architecture import source_inventory
from check_frozen_content import validate_payload
from check_evidence_backend_runtime import validate_sample as validate_evidence_sample

ROOT = Path(__file__).resolve().parents[2]


def validate():
    sources = source_inventory()
    report = json.loads((ROOT / '.local/architecture-check/postgres16-wsl.json').read_text(encoding='utf-8'))
    if not report.get('passed') or report.get('sources') != sources or not report.get('sources_unchanged_during_checks'):
        raise ValueError('stale or failed native evidence')
    samples = report['go_repository_proof'].get('current_read_samples', [])
    if len(samples) != 1:
        raise ValueError('missing actual read sample')
    spec = json.loads((ROOT / 'docs/architecture/openapi/partner.v1.json').read_text(encoding='utf-8'))
    validators = {name: Draft202012Validator({'$ref': '#/components/schemas/' + name, 'components': spec['components']}) for name in ('Block', 'Citation', 'ReadRequest', 'ReadResult')}
    for sample in samples:
        record, fact, projection = [sample[k] for k in ('record', 'published', 'projection')]
        payload = base64.b64decode(record['Payload'], validate=True)
        if payload != canonical(payload):
            raise ValueError('noncanonical content')
        content = json.loads(payload)
        validate_payload(content)
        hashes = frozen(payload)
        if hashes['content_hash'] != fact['ContentDigest'] or record['DocumentID'] != fact['DocumentID'] or record['RevisionID'] != fact['RevisionID']:
            raise ValueError('publication or content commitment differs')
        expected_blocks, expected_citations = [], []
        for block in content['blocks']:
            mapped = {k: v for k, v in block.items() if k != 'order'}
            mapped['revision_id'] = record['RevisionID']
            citation = {'document_id': record['DocumentID'], 'revision_id': record['RevisionID'], 'block_id': block['id'], 'source_anchor': block['source_anchor']}
            validators['Block'].validate(mapped)
            validators['Citation'].validate(citation)
            expected_blocks.append(mapped)
            expected_citations.append(citation)
        expected = {'document_id': record['DocumentID'], 'revision_id': record['RevisionID'], 'blocks': expected_blocks, 'citations': expected_citations}
        if projection != expected:
            raise ValueError('actual projection changed frozen fields or order')
        if sample['authentication'] != 'actual-rs256-current-status' or sample['authorization'] != 'postgres-explicit-read':
            raise ValueError('missing actual authentication/authorization')
        if sample.get('original_grant_calls', 0) < 4 or sample.get('original_projection_calls') != 1 or sample.get('original_authorization') != 'fixture-independent-read':
            raise ValueError('source verification missing or fixture boundary hidden')
        originals = {}
        for source in report['go_repository_proof']['evidence_backend_samples']:
            validate_evidence_sample(source, report['go_repository_proof'])
            blob, data = source['original_blob'], base64.b64decode(source['original_bytes'], validate=True)
            if len(data) != blob['Size'] or hashlib.sha256(data).hexdigest() != blob['SHA256']:
                raise ValueError('actual original bytes mismatch')
            originals[blob['ID']] = data.decode('utf-8')
        for block in content['blocks']:
            anchor = block['source_anchor']
            if block['kind'] != 'text' or anchor['kind'] != 'original' or anchor['locator'] != {'precision': 'coarse'} or not block['text'] or block['text'] not in originals[anchor['blob_id']]:
                raise ValueError('whole original block not independently supported')
        validators['ReadRequest'].validate(sample['rest_request'])
        rest = sample['rest_result']
        validators['ReadResult'].validate(rest)
        mcp = sample['mcp_result']['result']
        validators['ReadResult'].validate(mcp['structuredContent'])
        if mcp.get('isError', False) or len(mcp['content']) != 1 or json.loads(mcp['content'][0]['text']) != mcp['structuredContent']:
            raise ValueError('MCP read representations differ')
        rest_pages, mcp_pages, requests = sample['rest_pages'], sample['mcp_pages'], sample['page_requests']
        if len(rest_pages) != 2 or len(mcp_pages) != 2 or len(requests) != 2 or len(expected_blocks) != 2:
            raise ValueError('actual complete paging evidence missing')
        accumulated_blocks, accumulated_citations = [], []
        for index, (page, rpc, request) in enumerate(zip(rest_pages, mcp_pages, requests)):
            validators['ReadRequest'].validate(request)
            validators['ReadResult'].validate(page)
            result = rpc['result']
            value = result['structuredContent']
            validators['ReadResult'].validate(value)
            if result.get('isError', False) or len(result['content']) != 1 or json.loads(result['content'][0]['text']) != value:
                raise ValueError('paged MCP formats differ')
            for key in ('document_id', 'revision_id', 'blocks', 'citations', 'profile_digest', 'profile_version'):
                if value[key] != page[key]:
                    raise ValueError('REST/MCP page parity differs')
            if len(page['blocks']) != 1 or len(page['citations']) != 1:
                raise ValueError('page size differs')
            if page['blocks'][0] != expected_blocks[index] or page['citations'][0] != expected_citations[index]:
                raise ValueError('page altered frozen content/order')
            accumulated_blocks.extend(page['blocks'])
            accumulated_citations.extend(page['citations'])
            if index == 0:
                if not page.get('next_cursor') or requests[1].get('cursor') != page['next_cursor']:
                    raise ValueError('continuation not bound to actual first-page cursor')
            elif 'next_cursor' in page or 'next_cursor' in value:
                raise ValueError('last page did not terminate')
        if accumulated_blocks != expected_blocks or accumulated_citations != expected_citations:
            raise ValueError('paging completeness differs')
        if sample.get('tampered_cursor_rejected') is not True or sample.get('revoked_page_identity_rejected') is not True:
            raise ValueError('paging denial evidence missing')
        if rest['profile_digest'] != sample['request']['ProfileDigest'] or mcp['structuredContent']['profile_digest'] != rest['profile_digest']:
            raise ValueError('read profile binding changed')
        listed = sample['mcp_tools_list']['result']['tools']
        if {tool['name'] for tool in listed} != {'knowledge_search', 'knowledge_read'} or len(listed) != 2:
            raise ValueError('read tool runtime inventory differs')
        catalog = json.loads((ROOT / 'docs/architecture/mcp/tools.v1.json').read_text(encoding='utf-8'))
        defined = next(tool for tool in catalog['tools'] if tool['name'] == 'knowledge_read')
        actual = next(tool for tool in listed if tool['name'] == 'knowledge_read')
        for key in ('inputSchema', 'outputSchema'):
            if actual[key] != defined[key]:
                raise ValueError('read tool schema changed')
    if source_inventory() != sources:
        raise ValueError('sources changed')
    return {'passed': True, 'production_acceptance': False, 'scope': 'PG16 signed-token current read and two-page signed cursor continuation across REST/MCP, tamper/revoked identity rejection, actual UTF8 whole blocks and Schema/field parity; original grant fixture; no history, production bootstrap, performance or send barrier acceptance', 'sources': sources, 'sources_unchanged_during_checks': True, 'actual_samples': len(samples)}


if __name__ == '__main__':
    result = validate()
    (ROOT / '.local/architecture-check/current-read-runtime.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('PG16 current read: actual frozen content and partner projection independently validated')

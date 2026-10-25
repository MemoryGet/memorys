"""Verify the actual local CLI in an owned PostgreSQL cluster, including restore."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

from check_architecture import ROOT, source_inventory


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pg-bin', type=Path, required=True)
    parser.add_argument('--port', type=int, default=55439)
    parser.add_argument('--ai', action='store_true', help='also verify real local embeddings, original files and DeepSeek (synthetic evidence only)')
    parser.add_argument('--report', type=Path, default=ROOT / '.local/architecture-check/local-cli-postgres18-windows.json')
    args = parser.parse_args()
    if not args.pg_bin.is_absolute() or not 1024 <= args.port <= 65535:
        parser.error('absolute PostgreSQL binary directory and unprivileged port required')
    parent = (ROOT / '.local/architecture-check').resolve()
    parent.mkdir(parents=True, exist_ok=True)
    before = source_inventory()
    report = {'passed': False, 'production_acceptance': False, 'checks': [],
              'scope': 'isolated Windows PostgreSQL/local CLI lifecycle and backup restore',
              'server_stopped': False, 'temporary_resources_removed': False}
    folder = None
    attempted = False
    http_process = None
    env = dict(os.environ)
    for name in ('PGPASSWORD', 'PGSERVICE', 'PGSERVICEFILE', 'MEMORYS_DATABASE_URL',
                 'MEMORYS_LOCAL_TEST_DATABASE_URL'):
        env.pop(name, None)
    # The owned instance uses trust on a reserved loopback port, never the user's 5432.
    env.update(PGHOST='127.0.0.1', PGPORT=str(args.port), PGUSER='postgres', PGDATABASE='postgres')

    def pg(name):
        result = args.pg_bin / (name + '.exe')
        if not result.is_file():
            raise RuntimeError('missing PostgreSQL tool: ' + name)
        return result

    def run(command, local_env=None, timeout=120, success=True):
        if Path(str(command[0])).stem == 'pg_ctl':
            with (folder / 'controller.log').open('a', encoding='utf-8') as log:
                result = subprocess.run(list(map(str, command)), cwd=ROOT, env=local_env or env,
                                        stdout=log, stderr=subprocess.STDOUT, timeout=timeout)
        else:
            result = subprocess.run(list(map(str, command)), cwd=ROOT, env=local_env or env,
                                    capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=timeout)
            with (folder / 'commands.log').open('a', encoding='utf-8') as log:
                log.write(Path(str(command[0])).name + '\n' + result.stdout + result.stderr + '\n')
        if success and result.returncode:
            # Logs stay within the owned disposable fixture; reports contain no content or URL.
            detail = ''
            if Path(str(command[0])).name == 'memorys-local.exe':
                detail = ' ' + str(command[1]) + ': ' + result.stderr.strip()
            raise RuntimeError(Path(str(command[0])).name + ' failed; exit ' + str(result.returncode) + detail)
        return result

    def db_env(database):
        return dict(env, MEMORYS_DATABASE_URL=f'postgres://postgres@127.0.0.1:{args.port}/{database}?sslmode=disable')

    def stop_http():
        nonlocal http_process
        if http_process is not None:
            http_process.terminate()
            http_process.wait(timeout=10)
            http_process = None

    try:
        with socket.socket() as sock:
            if sock.connect_ex(('127.0.0.1', args.port)) == 0:
                raise RuntimeError('fixture port occupied; existing instance untouched')
        folder = Path(tempfile.mkdtemp(prefix='local-cli-pg-', dir=parent)).resolve()
        (folder / '.memorys-owned').write_text('disposable local CLI fixture\n')
        go = ROOT / '.tools/go/bin/go.exe'
        binary, migrate, test_binary = (folder / name for name in ('memorys-local.exe', 'migrate.exe', 'localcli.test.exe'))
        run([go, 'build', '-o', binary, './cmd/memorys-local'])
        run([go, 'build', '-o', migrate, './cmd/memorys-migrate'])
        run([go, 'test', '-c', '-o', test_binary, './pkg/localcli'])
        run([pg('initdb'), '-D', folder / 'data', '-U', 'postgres', '-A', 'trust', '--no-locale', '--encoding=UTF8'])
        attempted = True
        run([pg('pg_ctl'), '-D', folder / 'data', '-l', folder / 'server.log', '-w', '-t', '20', '-o',
             f'-h 127.0.0.1 -p {args.port} -c shared_buffers=32MB -c max_connections=30 -c jit=off', 'start'], timeout=40)
        report['server_version'] = run([pg('psql'), '-X', '-w', '-Atc', 'SHOW server_version']).stdout.strip()
        report['postgres_binary_sha256'] = hashlib.sha256(pg('postgres').read_bytes()).hexdigest()
        run([migrate], db_env('postgres'))
        test_env = dict(db_env('postgres'), MEMORYS_LOCAL_TEST_DATABASE_URL=db_env('postgres')['MEMORYS_DATABASE_URL'])
        tests = run([test_binary, '-test.v', '-test.run', '^TestPostgresLifecycle$'], test_env, timeout=240)
        required = ['recover_' + stage for stage in ('submitted', 'approved', 'started', 'claimed', 'prepared', 'completed')]
        required += ['expired_lease']
        if any('--- PASS: TestPostgresLifecycle/' + case not in tests.stdout for case in required):
            raise RuntimeError('fault recovery test evidence absent')
        report['checks'] += ['all_publication_checkpoint_recovery', 'expired_lease_new_fenced_attempt',
                             'invalid_and_revoked_tokens_401_no_evidence', 'current_acl_revocation', 'init_conflict_preserves_policy']

        run([pg('createdb'), '-w', 'cli_e2e'])
        local_env = db_env('cli_e2e')
        run([migrate], local_env)
        runtime = folder / 'runtime'

        def cli(command, *arguments, state=None, local_env=None, success=True):
            result = run([binary, command, '--state', state or runtime, *arguments], local_env or active_env[0], success=success)
            return json.loads(result.stdout) if success else result

        active_env = [local_env]
        first = cli('init', '--with-samples')
        second = cli('init', '--with-samples')
        if first['sample_document_id'] != second['sample_document_id']:
            raise RuntimeError('sample init duplication')
        if not cli('doctor')['checks']['searchable']:
            raise RuntimeError('doctor did not observe published data')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            http_port = sock.getsockname()[1]
        address = f'127.0.0.1:{http_port}'

        def start_http(state):
            nonlocal http_process
            with (folder / 'http.log').open('ab') as log:
                http_process = subprocess.Popen([str(binary), 'serve', '--state', str(state), '--address', address],
                                                cwd=ROOT, env=active_env[0], stdout=log, stderr=log,
                                                creationflags=subprocess.CREATE_NO_WINDOW)
            for _ in range(60):
                if http_process.poll() is not None:
                    raise RuntimeError('CLI HTTP host exited')
                try:
                    with urllib.request.urlopen('http://' + address + '/health', timeout=1) as response:
                        if response.status == 200:
                            return
                except OSError:
                    time.sleep(0.2)
            raise RuntimeError('CLI HTTP startup timeout')

        def search(query, state=None):
            return cli('search', query, '--address', address, state=state)['evidence']

        start_http(runtime)
        if len(search('memoryslocalsample')) != 1:
            raise RuntimeError('actual sample HTTP search absent')
        document = folder / 'own.md'
        document.write_text('localuserdocument\n\n用户自己的 Markdown **正文**。\n', encoding='utf-8')
        imported = cli('import', str(document))
        if search('localuserdocument'):
            raise RuntimeError('draft leaked into HTTP search')
        published = cli('publish', imported['draft_id'], '--expect-hash', imported['content_hash'])
        repeated = cli('publish', imported['draft_id'], '--expect-hash', imported['content_hash'])
        if published != repeated or not cli('import', str(document))['reused']:
            raise RuntimeError('CLI replay differs')
        evidence = search('localuserdocument')
        if len(evidence) != 1 or '用户自己的 Markdown **正文**。' not in evidence[0]['source_excerpt']:
            raise RuntimeError('HTTP evidence does not match imported content')
        report['checks'] += ['actual_cli_sample_init_idempotent', 'actual_cli_draft_hidden',
                             'actual_cli_import_publish_replay', 'actual_http_revision_excerpt_matches_source']

        large = folder / 'bounded.txt'
        text = ('localmaximumprobe\n' + 'boundedtext ' * (256 * 1024))[:256 * 1024]
        # write_text translates LF to CRLF on Windows, accidentally adding a
        # byte beyond this exact boundary. Test the accepted byte limit itself.
        large.write_bytes(text.encode('utf-8'))
        if large.stat().st_size != 256 * 1024:
            raise RuntimeError('maximum-input fixture byte length differs')
        big = cli('import', str(large))
        cli('publish', big['draft_id'], '--expect-hash', big['content_hash'])
        if len(search('localmaximumprobe')) != 1:
            raise RuntimeError('maximum accepted input not searchable')
        report['checks'].append('256_kib_document_publication')
        stop_http()
        start_http(runtime)
        if search('localuserdocument') != evidence:
            raise RuntimeError('restart changed source evidence')
        report['checks'].append('actual_cli_host_restart')
        stop_http()
        run([pg('pg_dump'), '-w', '-d', 'cli_e2e', '-Fc', '-f', folder / 'database.dump'])
        run([pg('createdb'), '-w', 'cli_restored'])
        run([pg('pg_restore'), '-w', '--exit-on-error', '--no-owner', '--no-acl', '-d', 'cli_restored', folder / 'database.dump'])
        moved = folder / 'moved' / 'runtime'
        shutil.copytree(runtime, moved)
        active_env[0] = db_env('cli_restored')
        cli('init', state=moved)  # Also reapply owner-only key permissions after copying.
        if not cli('doctor', state=moved)['checks']['searchable']:
            raise RuntimeError('restored configuration rejected')
        start_http(moved)
        if search('localuserdocument', state=moved) != evidence:
            raise RuntimeError('restored source evidence differs')
        if cli('init', state=folder / 'unrelated-state', success=False).returncode == 0:
            raise RuntimeError('new install silently adopted restored database')
        report['checks'] += ['pg_dump_pg_restore_fresh_database', 'runtime_directory_relocation',
                             'matching_state_required_after_restore']
        stop_http()
        if args.ai:
            fixtures = folder / 'originals'
            run([ROOT / '.local/markitdown-env/Scripts/python.exe',
                 ROOT / 'docs/architecture/local_ai_fixtures.py', fixtures])
            run([pg('createdb'), '-w', 'ai_guards'])
            guards_env = db_env('ai_guards')
            run([migrate], guards_env)
            guards_env.update(MEMORYS_LOCAL_AI_TEST_DATABASE_URL=guards_env['MEMORYS_DATABASE_URL'],
                              MEMORYS_AI_FIXTURE_ROOT=str(fixtures))
            run([test_binary, '-test.v', '-test.run', '^TestPostgresAIGuards$'], guards_env, timeout=240)
            report['checks'] += ['ai_policy_upgrade_recovery', 'native_corpus_mutation_rejected',
                                 'revocation_during_embedding_and_generation_withholds_output',
                                 'fabricated_citation_rejected', 'independent_original_read_revocation']
            run([pg('createdb'), '-w', 'cli_ai'])
            active_env[0] = db_env('cli_ai')
            run([migrate], active_env[0])
            runtime = folder / 'ai-runtime'
            cli('init', '--with-samples')
            enabled = cli('enable-ai')
            if cli('enable-ai') != enabled:
                raise RuntimeError('AI upgrade replay changed configuration')
            report['embedding'] = enabled['embedding']
            start_http(runtime)
            if len(search('memoryslocalsample')) != 1:
                raise RuntimeError('AI upgrade broke existing keyword data')
            sources = {}
            for extension in ('pdf', 'docx', 'xlsx', 'pptx'):
                marker = 'local' + extension + 'probe'
                item = cli('import', str(fixtures / ('sample.' + extension)))
                if search(marker):
                    raise RuntimeError('original draft leaked before approval')
                published = cli('publish', item['draft_id'], '--expect-hash', item['content_hash'])
                if cli('publish', item['draft_id'], '--expect-hash', item['content_hash']) != published:
                    raise RuntimeError('original publication replay changed')
                if not cli('import', str(fixtures / ('sample.' + extension)))['reused']:
                    raise RuntimeError('original reimport duplicated document')
                found = search(marker)
                if len(found) != 1 or found[0]['citation']['source_anchor']['kind'] != 'original' or marker not in found[0]['source_excerpt']:
                    raise RuntimeError('original citation or real source excerpt differs: ' + extension)
                sources[extension] = found
                report['checks'].append('actual_' + extension + '_import_approve_publish_http_source')
                print('AI original lifecycle: ' + extension + ' passed', flush=True)
            question = '星帆项目发生紧急情况时，用什么口令联系？'
            ranked = cli('semantic-search', question)
            if not ranked['evidence'] or ranked['evidence'][0]['Citation']['DocumentID'] != sources['docx'][0]['citation']['document_id']:
                raise RuntimeError('real multilingual embedding did not rank the expected synthetic source first')
            if '紫色海豚' not in ranked['evidence'][0]['Text']:
                raise RuntimeError('semantic source text changed')
            answered = cli('ask', question)
            if answered['status'] != 'answered' or '紫色海豚' not in answered['answer'] or not answered['citations']:
                raise RuntimeError('actual DeepSeek answer or citation absent')
            expected = {item['Citation']['DocumentID'] for item in answered['evidence']}
            if any(c['DocumentID'] not in expected for c in answered['citations']):
                raise RuntimeError('model citation escaped authorized evidence')
            report['answer_model'] = answered['answer_model']
            report['checks'] += ['real_multilingual_semantic_ranking', 'real_deepseek_answer_with_verified_citations']
            print('AI real semantic ranking and DeepSeek answer: passed', flush=True)
            stop_http()
            # Actual damaged original bytes must prevent output, then recover.
            blob_id = sources['pdf'][0]['citation']['source_anchor']['blob_id']
            blobs = list((runtime / 'blobs').rglob(blob_id))
            if len(blobs) != 1 or blobs[0].read_bytes() != (fixtures / 'sample.pdf').read_bytes():
                raise RuntimeError('approved original file absent or differs from source fixture')
            original = blobs[0].read_bytes()
            blobs[0].write_bytes(b'corrupt original')
            if cli('semantic-search', question, success=False).returncode == 0:
                raise RuntimeError('corrupted original released semantic evidence')
            blobs[0].write_bytes(original)
            report['checks'].append('tampered_original_withholds_semantic_output')
            run([pg('pg_dump'), '-w', '-d', 'cli_ai', '-Fc', '-f', folder / 'ai.dump'])
            run([pg('createdb'), '-w', 'ai_restored'])
            run([pg('pg_restore'), '-w', '--exit-on-error', '--no-owner', '--no-acl', '-d', 'ai_restored', folder / 'ai.dump'])
            ai_moved = folder / 'ai-moved' / 'runtime'
            shutil.copytree(runtime, ai_moved)
            active_env[0] = db_env('ai_restored')
            cli('init', state=ai_moved)
            if not cli('doctor', state=ai_moved)['checks']['embedding_model']:
                raise RuntimeError('restored AI diagnostic rejected pinned model')
            start_http(ai_moved)
            for extension, evidence in sources.items():
                if search('local' + extension + 'probe', state=ai_moved) != evidence:
                    raise RuntimeError('restored original evidence differs: ' + extension)
            if cli('semantic-search', question, state=ai_moved)['evidence'] != ranked['evidence']:
                raise RuntimeError('restored semantic ranking differs')
            stop_http()
            report['checks'] += ['ai_originals_database_and_state_restore', 'ai_semantic_cache_relocation']
        # Copies of private key material are intentionally never put in the report.
        report['passed'] = True
    except Exception as error:
        report['error'] = str(error)
        if folder and (folder / 'commands.log').is_file():
            # Print only the relevant test failure lines, not document or credential payloads.
            lines = (folder / 'commands.log').read_text(encoding='utf-8').splitlines()
            print('\n'.join(line for line in lines if 'lifecycle_test.go:' in line or line.startswith('--- FAIL')))
    finally:
        try:
            stop_http()
            if folder and attempted:
                result = run([pg('pg_ctl'), '-D', folder / 'data', '-m', 'fast', '-w', '-t', '20', 'stop'], success=False, timeout=40)
                if result.returncode and run([pg('pg_ctl'), '-D', folder / 'data', 'status'], success=False).returncode != 3:
                    raise RuntimeError('owned PostgreSQL still running; retained files')
            report['server_stopped'] = True
            if folder:
                if folder.parent != parent or not folder.name.startswith('local-cli-pg-') or not (folder / '.memorys-owned').is_file():
                    raise RuntimeError('temporary directory ownership check failed')
                shutil.rmtree(folder)
                report['temporary_resources_removed'] = True
        except Exception as error:
            report['cleanup_error'] = str(error)
            report['passed'] = False
        report['sources'] = before
        report['sources_unchanged_during_checks'] = before == source_inventory()
        if not report['sources_unchanged_during_checks']:
            report['passed'] = False
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('Local CLI PostgreSQL lifecycle: ' + ('passed' if report['passed'] else 'failed'))
    if not report['passed']:
        print(report.get('error', report.get('cleanup_error', 'source inputs changed during checks')))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())

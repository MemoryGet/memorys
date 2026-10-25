"""Real browser/HTTP acceptance in an owned PostgreSQL 18 fixture, never user DB."""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

from check_architecture import ROOT, source_inventory


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pg-bin', type=Path, required=True)
    parser.add_argument('--port', type=int, default=55441)
    parser.add_argument('--report', type=Path, default=ROOT / '.local/architecture-check/local-web-postgres18-windows.json')
    args = parser.parse_args()
    if not args.pg_bin.is_absolute() or not 1024 <= args.port <= 65535:
        parser.error('absolute PostgreSQL binary path and unprivileged port required')
    parent = (ROOT / '.local/architecture-check').resolve()
    parent.mkdir(parents=True, exist_ok=True)
    sources = source_inventory()
    assets = ROOT / 'dashboard/dist'
    asset_hashes = {p.relative_to(assets).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in assets.rglob('*') if p.is_file()}
    report = {'passed': False, 'production_acceptance': False, 'checks': [], 'scope': 'local owner web; real browser, synthetic sources, actual local embeddings and DeepSeek'}
    folder, started, process = None, False, None
    env = dict(os.environ)
    for name in ('PGPASSWORD', 'PGSERVICE', 'PGSERVICEFILE', 'MEMORYS_DATABASE_URL'):
        env.pop(name, None)
    env.update(PGHOST='127.0.0.1', PGPORT=str(args.port), PGUSER='postgres', PGDATABASE='postgres',
               MEMORYS_DATABASE_URL=f'postgres://postgres@127.0.0.1:{args.port}/postgres?sslmode=disable')

    def pg(name):
        p = args.pg_bin / (name + '.exe')
        if not p.is_file(): raise RuntimeError('missing PostgreSQL tool: ' + name)
        return p

    def run(command, timeout=120, local_env=None):
        with (folder / 'commands.log').open('a', encoding='utf-8') as log:
            result = subprocess.run(list(map(str, command)), cwd=ROOT, env=local_env or env, stdout=log, stderr=subprocess.STDOUT, timeout=timeout)
        if result.returncode:
            detail = ''
            if Path(str(command[0])).name == 'memorys-local.exe':
                # CLI errors are static and redact database/model credentials.
                detail = ' ' + str(command[1]) + ': ' + (folder / 'commands.log').read_text(encoding='utf-8').splitlines()[-1]
            raise RuntimeError(Path(str(command[0])).name + ' failed' + detail)

    def stop_web():
        nonlocal process
        if process:
            process.terminate()
            process.wait(timeout=10)
            process = None

    try:
        if not (assets / 'index.html').is_file(): raise RuntimeError('build dashboard first')
        with socket.socket() as sock:
            if sock.connect_ex(('127.0.0.1', args.port)) == 0: raise RuntimeError('fixture port occupied; existing database untouched')
        folder = Path(tempfile.mkdtemp(prefix='local-web-pg-', dir=parent)).resolve()
        (folder / '.memorys-owned').write_text('owned disposable web fixture\n')
        binary = folder / 'memorys-local.exe'
        migrate = folder / 'migrate.exe'
        go = ROOT / '.tools/go/bin/go.exe'
        run([go, 'build', '-o', binary, './cmd/memorys-local'])
        run([go, 'build', '-o', migrate, './cmd/memorys-migrate'])
        run([pg('initdb'), '-D', folder / 'data', '-U', 'postgres', '-A', 'trust', '--no-locale', '--encoding=UTF8'])
        started = True
        run([pg('pg_ctl'), '-D', folder / 'data', '-l', folder / 'server.log', '-w', '-t', '20', '-o', f'-h 127.0.0.1 -p {args.port} -c shared_buffers=32MB -c max_connections=30 -c jit=off', 'start'])
        report['postgres_version'] = subprocess.check_output([str(pg('psql')), '-X', '-w', '-Atc', 'SHOW server_version'], env=env, cwd=ROOT, text=True).strip()
        run([migrate])
        runtime = folder / 'runtime'
        run([binary, 'init', '--with-samples', '--state', runtime])
        run([binary, 'enable-ai', '--state', runtime])
        report['embedding'] = json.loads((runtime / 'manifest.json').read_text(encoding='utf-8'))['Embedding']
        fixtures = folder / 'originals'
        run([ROOT / '.local/markitdown-env/Scripts/python.exe', ROOT / 'docs/architecture/local_ai_fixtures.py', fixtures])
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            address = f'127.0.0.1:{sock.getsockname()[1]}'
        origin = 'http://' + address

        def start_web(state):
            nonlocal process
            with (folder / 'web.log').open('ab') as log:
                process = subprocess.Popen([str(binary), 'web', '--state', str(state), '--address', address, '--ui-dir', str(assets)], cwd=ROOT, env=env, stdout=log, stderr=log, creationflags=subprocess.CREATE_NO_WINDOW)
            for _ in range(80):
                if process.poll() is not None: raise RuntimeError('web host exited')
                try:
                    with urllib.request.urlopen(origin + '/health', timeout=1) as response:
                        if response.status == 200: return
                except OSError: time.sleep(0.2)
            raise RuntimeError('web startup timeout')

        start_web(runtime)
        artifacts = parent / 'local-web-browser'
        with (folder / 'browser.log').open('w', encoding='utf-8') as log:
            result = subprocess.run(['node', str(ROOT / 'docs/architecture/local_web_browser.cjs'), origin, str(fixtures), str(artifacts)], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=240)
        if result.returncode:
            message = (folder / 'browser.log').read_text(encoding='utf-8')[-3000:]
            raise RuntimeError('browser acceptance failed: ' + message)
        browser = json.loads((folder / 'browser.log').read_text(encoding='utf-8').strip())
        if not browser.get('passed'): raise RuntimeError('browser evidence absent')
        report['checks'] += browser['checks']
        print('Real browser upload/publication/search/answer: passed', flush=True)

        def local_search(query, mode):
            jar = http.cookiejar.CookieJar()
            opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar), urllib.request.ProxyHandler({}))
            opener.open(origin + '/local/', timeout=5).close()
            request = urllib.request.Request(origin + '/local/api/search', data=json.dumps({'query': query, 'mode': mode}).encode(), headers={'Content-Type': 'application/json', 'Origin': origin, 'X-Memorys-Local': '1'})
            with opener.open(request, timeout=120) as response: return json.load(response)

        original = local_search('localdocxprobe', 'keyword')['evidence']
        if len(original) != 1 or original[0]['citation']['source_kind'] != 'original': raise RuntimeError('expected original HTTP source absent')
        stop_web()
        run([pg('pg_dump'), '-w', '-d', 'postgres', '-Fc', '-f', folder / 'web.dump'])
        run([pg('createdb'), '-w', 'web_restored'])
        run([pg('pg_restore'), '-w', '--exit-on-error', '--no-owner', '--no-acl', '-d', 'web_restored', folder / 'web.dump'])
        moved = folder / 'moved/runtime'
        shutil.copytree(runtime, moved)
        env['MEMORYS_DATABASE_URL'] = f'postgres://postgres@127.0.0.1:{args.port}/web_restored?sslmode=disable'
        run([binary, 'init', '--state', moved])
        start_web(moved)
        if local_search('localdocxprobe', 'keyword')['evidence'] != original: raise RuntimeError('restored web original citation differs')
        if '琥珀灯塔' not in local_search('远舟计划出现紧急状况时，应该使用什么联络暗号？', 'semantic')['evidence'][0]['source_excerpt']: raise RuntimeError('restored web semantic source differs')
        report['checks'] += ['web_host_restart_after_pg_restore_and_runtime_relocation', 'restored_original_http_and_local_semantic_evidence']
        # Only the owned restored fixture is changed. A running web host must
        # honor a disabled owner instead of using its signing key as permission.
        run([pg('psql'), '-X', '-w', '-d', 'web_restored', '-v', 'ON_ERROR_STOP=1', '-c',
             "UPDATE akos_withdraw_v1.identity_subjects SET enabled=false WHERE subject_type='user'"])
        try:
            local_search('localdocxprobe', 'keyword')
            raise RuntimeError('disabled owner still read web evidence')
        except urllib.error.HTTPError as denied:
            if denied.code not in (400, 403, 503) or 'evidence' in json.load(denied):
                raise RuntimeError('disabled owner response was not safely rejected')
        report['checks'].append('running_web_host_rejects_disabled_owner_without_evidence')
        stop_web()
        report['passed'] = True
    except Exception as error:
        report['error'] = str(error)
    finally:
        try:
            stop_web()
            if folder and started:
                run([pg('pg_ctl'), '-D', folder / 'data', '-m', 'fast', '-w', '-t', '20', 'stop'])
            report['server_stopped'] = True
            if folder:
                if folder.parent != parent or not folder.name.startswith('local-web-pg-') or not (folder / '.memorys-owned').is_file(): raise RuntimeError('fixture ownership mismatch')
                shutil.rmtree(folder)
                report['temporary_resources_removed'] = True
        except Exception as error:
            report.update(passed=False, cleanup_error=str(error))
        report['sources'], report['frontend_build'] = sources, asset_hashes
        report['sources_unchanged_during_checks'] = sources == source_inventory()
        report['frontend_unchanged_during_checks'] = asset_hashes == {p.relative_to(assets).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in assets.rglob('*') if p.is_file()}
        if not report['sources_unchanged_during_checks'] or not report['frontend_unchanged_during_checks']: report['passed'] = False
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('Local web PostgreSQL/browser: ' + ('passed' if report['passed'] else 'failed'))
    if not report['passed']: print(report.get('error', report.get('cleanup_error', 'sources changed')))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())

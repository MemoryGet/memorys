"""Backup tool acceptance on an owned PostgreSQL instance; no user DB access."""
import argparse
import importlib.util
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
    parser.add_argument('--port', type=int, default=55443)
    parser.add_argument('--report', type=Path, default=ROOT / '.local/architecture-check/local-backup-postgres18-windows.json')
    args = parser.parse_args()
    parent = (ROOT / '.local/architecture-check').resolve()
    parent.mkdir(parents=True, exist_ok=True)
    sources = source_inventory()
    report = {'passed': False, 'production_acceptance': False, 'checks': [], 'scope': 'owned offline backup, private files, new database restore, actual HTTP and local semantic retrieval'}
    folder, started, http = None, False, None
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith('PG') and k != 'MEMORYS_DATABASE_URL'}
    env.update(PGHOST='127.0.0.1', PGPORT=str(args.port), PGUSER='postgres', PGDATABASE='postgres',
               MEMORYS_DATABASE_URL=f'postgres://postgres@127.0.0.1:{args.port}/postgres?sslmode=disable')

    def pg(name):
        return args.pg_bin / (name + '.exe')

    def run(command, local_env=None, success=True, timeout=180):
        if Path(str(command[0])).stem == 'pg_ctl':
            with (folder / 'pg-controller.log').open('ab') as log:
                result = subprocess.run(list(map(str, command)), env=local_env or env, cwd=ROOT, stdout=log, stderr=log, timeout=timeout)
        else:
            result = subprocess.run(list(map(str, command)), env=local_env or env, cwd=ROOT, capture_output=True, timeout=timeout)
        if success and result.returncode:
            # Do not print model content, credentials, private files or DB errors.
            name = Path(str(command[0])).name.lower()
            message = result.stderr.decode('utf-8', errors='replace')[-1000:] if name.startswith('python') or name == 'memorys-local.exe' else ''
            if name == 'memorys-local.exe': message = str(command[1]) + ': ' + message
            raise RuntimeError('Owned fixture command failed: ' + Path(str(command[0])).name + ' ' + message)
        return result

    def cli(command, *rest, workspace=None, local_env=None):
        where = workspace or folder / 'app'
        result = run([where / '.local/bin/memorys-local.exe', command, '--state', where / '.local/runtime', *rest], local_env)
        return json.loads(result.stdout)

    def stop_http():
        nonlocal http
        if http:
            http.terminate()
            http.wait(timeout=10)
            http = None

    try:
        if not args.pg_bin.is_absolute() or not 1024 <= args.port <= 65535:
            raise RuntimeError('absolute pg-bin and unused unprivileged port required')
        with socket.socket() as probe:
            if probe.connect_ex(('127.0.0.1', args.port)) == 0:
                raise RuntimeError('fixture port occupied; existing process untouched')
        folder = Path(tempfile.mkdtemp(prefix='local-backup-pg-', dir=parent)).resolve()
        (folder / '.memorys-owned').write_text('disposable backup fixture')
        app = folder / 'app'
        (app / '.local/bin').mkdir(parents=True)
        (app / 'tools/parsers').mkdir(parents=True)
        for name in ('tools/start-local-web.ps1', 'tools/parsers/markitdown_worker.py', 'tools/local_backup.py', 'docs/architecture/markitdown-requirements.lock.txt'):
            (app / name).parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, app / name)
        shutil.copytree(ROOT / 'dashboard/dist', app / 'dashboard/dist')
        go = ROOT / '.tools/go/bin/go.exe'
        run([go, 'build', '-o', app / '.local/bin/memorys-local.exe', './cmd/memorys-local'])
        migrate = folder / 'migrate.exe'
        run([go, 'build', '-o', migrate, './cmd/memorys-migrate'])
        run([pg('initdb'), '-D', folder / 'data', '-U', 'postgres', '-A', 'trust', '--no-locale', '--encoding=UTF8'])
        started = True
        run([pg('pg_ctl'), '-D', folder / 'data', '-l', folder / 'server.log', '-w', '-t', '20', '-o', f'-h 127.0.0.1 -p {args.port} -c shared_buffers=32MB -c max_connections=30 -c jit=off', 'start'])
        report['postgres_version'] = run([pg('psql'), '-X', '-w', '-Atc', 'SHOW server_version']).stdout.decode().strip()
        run([migrate])
        cli('init', '--with-samples')
        cli('enable-ai')
        fixtures = folder / 'fixtures'
        run([ROOT / '.local/markitdown-env/Scripts/python.exe', ROOT / 'docs/architecture/local_ai_fixtures.py', fixtures])
        imported = cli('import', fixtures / 'sample.docx')
        cli('publish', imported['draft_id'], '--expect-hash', imported['content_hash'])
        question = '星帆项目发生紧急情况时，用什么口令联系？'
        expected_semantic = cli('semantic-search', question)['evidence']

        # Load the same public utility used by users; backup executes actual
        # pg_dump and restore executes actual createdb/pg_restore.
        spec = importlib.util.spec_from_file_location('snapshot_tool', ROOT / 'tools/local_backup.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        previous = {key: os.environ.get(key) for key in ('MEMORYS_DATABASE_URL', 'PGPASSWORD')}
        os.environ['MEMORYS_DATABASE_URL'] = env['MEMORYS_DATABASE_URL']
        os.environ.pop('PGPASSWORD', None)
        try:
            bundle = folder / 'backup'
            module.backup(bundle, args.pg_bin, workspace=app, offline=True)
            data = module.verify(bundle)
            if not any(name.startswith('.local/runtime/blobs/') for name in data['files']):
                raise RuntimeError('original bytes absent from backup')
            report['checks'] += ['actual_offline_pg_dump_with_platform_write_barrier', 'complete_runtime_originals_and_application_hashes']
            # A second independent session cannot mutate while the barrier is held.
            conn = module.connection()
            with module.Postgres(args.pg_bin, conn).frozen():
                denied = run([pg('psql'), '-X', '-w', '-v', 'ON_ERROR_STOP=1', '-c', "SET lock_timeout='100ms'; UPDATE akos_withdraw_v1.identity_subjects SET enabled=enabled"], success=False)
                if denied.returncode == 0:
                    raise RuntimeError('write barrier did not block a separate writer')
            report['checks'].append('separate_database_writer_blocked_during_snapshot')
            if os.name == 'nt':
                rights = subprocess.check_output(['icacls', str(bundle)], text=True)
                if '(I)' in rights or 'Everyone:' in rights or 'BUILTIN\\Users:' in rights:
                    raise RuntimeError('backup inherited broad Windows access')
            report['checks'].append('private_backup_directory_without_inherited_acl')
            changed = bundle / '.local/runtime/private.pem'
            original = changed.read_bytes()
            changed.write_bytes(b'corrupt')
            try:
                module.restore(bundle, folder / 'must-not-exist', 'corrupt_restore', args.pg_bin)
                raise RuntimeError('corrupt private key accepted')
            except module.BackupError:
                if (folder / 'must-not-exist').exists():
                    raise RuntimeError('corruption touched destination before verification')
            changed.write_bytes(original)
            report['checks'].append('corrupt_backup_rejected_before_any_destination_or_db_write')
            restored = folder / 'restored'
            module.restore(bundle, restored, 'backup_restored', args.pg_bin)
            report['checks'].append('new_database_restore_and_matching_identity_configuration')
            try:
                module.restore(bundle, restored, 'must_not_create', args.pg_bin)
                raise RuntimeError('existing destination accepted')
            except FileExistsError:
                pass
            # Existing source database must not be replaced, even with a new directory.
            try:
                module.restore(bundle, folder / 'refused-existing-db', 'backup_restored', args.pg_bin)
                raise RuntimeError('existing database accepted')
            except module.BackupError:
                pass
            report['checks'].append('existing_destination_and_database_rejected_without_overwrite')
        finally:
            for key, value in previous.items():
                if value is None: os.environ.pop(key, None)
                else: os.environ[key] = value

        restored_env = dict(env, MEMORYS_DATABASE_URL=f'postgres://postgres@127.0.0.1:{args.port}/backup_restored?sslmode=disable')
        if cli('semantic-search', question, workspace=restored, local_env=restored_env)['evidence'] != expected_semantic:
            raise RuntimeError('restored actual semantic evidence differs')
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0))
            address = f'127.0.0.1:{probe.getsockname()[1]}'
        with (folder / 'http.log').open('ab') as log:
            http = subprocess.Popen([str(restored / '.local/bin/memorys-local.exe'), 'serve', '--state', str(restored / '.local/runtime'), '--address', address], cwd=ROOT, env=restored_env, stdout=log, stderr=log, creationflags=subprocess.CREATE_NO_WINDOW)
        for _ in range(60):
            if http.poll() is not None: raise RuntimeError('restored HTTP host exited')
            try:
                with urllib.request.urlopen('http://' + address + '/health', timeout=1) as result:
                    if result.status == 200: break
            except OSError: time.sleep(0.2)
        results = cli('search', 'localdocxprobe', '--address', address, workspace=restored, local_env=restored_env)['evidence']
        if len(results) != 1 or results[0]['citation']['source_anchor']['kind'] != 'original' or '紫色海豚' not in results[0]['source_excerpt']:
            raise RuntimeError('restored real HTTP original citation differs')
        report['checks'] += ['restored_actual_pinned_local_semantic_evidence_matches', 'restored_actual_http_original_excerpt_and_citation']
        stop_http()
        report['passed'] = True
    except Exception as error:
        report['error'] = str(error)
    finally:
        try:
            stop_http()
            if folder and started:
                run([pg('pg_ctl'), '-D', folder / 'data', '-m', 'fast', '-w', '-t', '20', 'stop'], timeout=40)
            report['server_stopped'] = True
            if folder:
                if folder.parent != parent or not folder.name.startswith('local-backup-pg-') or not (folder / '.memorys-owned').is_file():
                    raise RuntimeError('cleanup ownership check failed')
                shutil.rmtree(folder)
                report['temporary_resources_removed'] = True
        except Exception as error:
            report.update(passed=False, cleanup_error=str(error))
        report['sources'] = sources
        report['sources_unchanged_during_checks'] = sources == source_inventory()
        if not report['sources_unchanged_during_checks']: report['passed'] = False
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('Local offline backup/restore: ' + ('passed' if report['passed'] else 'failed'))
    if not report['passed']: print(report.get('error', report.get('cleanup_error', 'sources changed')))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())

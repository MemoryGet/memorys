"""Owned disposable Windows PostgreSQL SQL/repository check, never production acceptance."""
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

from check_architecture import ROOT, source_inventory


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pg-bin', type=Path, required=True)
    parser.add_argument('--port', type=int, default=55438)
    parser.add_argument('--report', type=Path, default=ROOT / '.local/architecture-check/postgres-windows.json')
    args = parser.parse_args()
    if not args.pg_bin.is_absolute() or not 1024 <= args.port <= 65535:
        parser.error('absolute binary directory and unprivileged port required')
    report = {'passed': False, 'production_acceptance': False,
              'scope': 'Windows isolated SQL/repository/local keyword search fixture',
              'server_stopped': False, 'temporary_resources_removed': False}
    before = source_inventory()
    parent = ROOT / '.local/architecture-check'
    parent.mkdir(parents=True, exist_ok=True)
    folder = None
    attempted = False

    def run(command, timeout=90, check=True, env=None):
        # Windows postmaster children may inherit controller pipe handles.
        # Redirect pg_ctl into an owned file so communicate() cannot wait for
        # a pipe EOF held by a long-lived, successfully started database.
        if Path(str(command[0])).stem == 'pg_ctl' and folder:
            with (folder / 'controller.log').open('a', encoding='utf-8') as output:
                result = subprocess.run([str(x) for x in command], cwd=ROOT, env=env,
                                        stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
        else:
            result = subprocess.run([str(x) for x in command], cwd=ROOT, env=env,
                                    capture_output=True, text=True, encoding='utf-8',
                                    errors='replace', timeout=timeout)
        if check and result.returncode:
            # Do not include SQL, parameters, credentials or content-bearing DETAILs.
            raise RuntimeError(f'{Path(str(command[0])).name} failed: ' + (result.stderr or '')[-1200:])
        return result

    def pg(name):
        path = args.pg_bin / (name + '.exe')
        if not path.is_file():
            raise RuntimeError('PostgreSQL executable absent: ' + name)
        return path

    try:
        with socket.socket() as sock:
            sock.settimeout(1)
            if sock.connect_ex(('127.0.0.1', args.port)) == 0:
                raise RuntimeError('fixture port already occupied; existing process untouched')
        report['binary_version'] = run([pg('postgres'), '--version']).stdout.strip()
        report['postgres_binary_sha256'] = hashlib.sha256(pg('postgres').read_bytes()).hexdigest()
        folder = Path(tempfile.mkdtemp(prefix='pg-windows-', dir=parent)).resolve()
        (folder / '.memorys-owned').write_text('disposable integration fixture\n')
        executable = folder / 'probe.exe'
        run([ROOT / '.tools/go/bin/go.exe', 'build', '-o', executable,
             './cmd/architecture-pg-repositories'], timeout=120)
        data = folder / 'data'
        run([pg('initdb'), '-D', data, '-U', 'postgres', '-A', 'trust', '--no-locale', '--encoding=UTF8'])
        attempted = True
        options = f'-h 127.0.0.1 -p {args.port} -c shared_buffers=32MB -c max_connections=20 -c jit=off'
        run([pg('pg_ctl'), '-D', data, '-l', folder / 'server.log', '-w', '-t', '20', '-o', options, 'start'], timeout=40)
        psql = [pg('psql'), '-X', '-w', '-h', '127.0.0.1', '-p', args.port,
                '-U', 'postgres', '-d', 'postgres', '-v', 'ON_ERROR_STOP=1']
        report['server_version'] = run(psql + ['-Atc', 'SHOW server_version']).stdout.strip()
        report['sql_checks'] = []
        for name in ('withdraw_contract_v1.sql', 'withdraw_contract_test.sql'):
            output = run(psql + ['-f', ROOT / 'docs/architecture/sql' / name]).stdout
            if name.endswith('_test.sql') and 'PASS: PostgreSQL withdrawal constraint prototype' not in output:
                raise RuntimeError('SQL completion marker absent')
            report['sql_checks'].append(name)
        env = dict(os.environ, AKOS_TEST_PG_PORT=str(args.port), AKOS_TEST_ROOT=str(ROOT), AKOS_TEST_GOVERNANCE_REPRO='')
        # Never inherit a user's local-server password into the owned trust fixture.
        env.pop('PGPASSWORD', None)
        env.pop('MEMORYS_DATABASE_URL', None)
        proof = json.loads(run([executable], timeout=90, env=env).stdout)
        if proof.get('passed') is not True or proof.get('production_acceptance') is not False:
            raise RuntimeError('repository proof rejected')
        report['checks'] = proof['checks']
        report['fixture_clock_observation'] = proof['fixture_clock_observation']
        report['local_runtime_search_samples'] = proof.get('local_runtime_search_samples', [])
        if not report['local_runtime_search_samples']:
            raise RuntimeError('local keyword search evidence absent')
        report['passed'] = True
    except Exception as error:
        report['error'] = str(error)
    finally:
        if folder:
            try:
                if attempted:
                    result = run([pg('pg_ctl'), '-D', folder / 'data', '-m', 'fast', '-w', '-t', '20', 'stop'], timeout=40, check=False)
                    if result.returncode and run([pg('pg_ctl'), '-D', folder / 'data', 'status'], check=False).returncode != 3:
                        raise RuntimeError('owned fixture still running; files retained')
                report['server_stopped'] = True
                if folder.parent != parent.resolve() or not folder.name.startswith('pg-windows-') or not (folder / '.memorys-owned').is_file():
                    raise RuntimeError('fixture ownership check failed')
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
    print('Windows PostgreSQL fixture: ' + ('passed' if report['passed'] else 'failed'))
    if not report['passed']:
        print(report.get('error', report.get('cleanup_error', 'source inputs changed')))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())

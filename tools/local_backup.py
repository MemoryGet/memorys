"""Offline local Memorys backup/restore. Standard library and PostgreSQL tools only."""
import argparse
import contextlib
import csv
import ctypes
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import sys
import threading
from urllib.parse import parse_qs, unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
FORMAT = 'memorys.local-backup/1'
LIMIT = 4 * 1024**3
MAX_FILES = 100000
CORE = {'.local/runtime/' + n for n in ('manifest.json', 'profile.json', 'jwks.json', 'private.pem')}
REQUIRED = CORE | {'database.dump', '.local/bin/memorys-local.exe', 'dashboard/dist/index.html',
                   'tools/start-local-web.ps1', 'tools/parsers/markitdown_worker.py',
                   'tools/local_backup.py', 'docs/architecture/markitdown-requirements.lock.txt'}
NAME = re.compile(r'[A-Za-z_][A-Za-z0-9_]{0,62}\Z')


class BackupError(Exception):
    pass


def fail(message):
    raise BackupError(message)


def regular(path):
    info = path.lstat()
    if path.is_symlink() or getattr(info, 'st_file_attributes', 0) & 0x400:
        fail('Links and Windows reparse points are not supported.')
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        fail('Only ordinary files with one link are supported.')
    return info.st_size


def secure_directory(path):
    """Must be called on a new, empty directory, before private bytes are copied."""
    path.mkdir(mode=0o700)
    if os.name != 'nt':
        return
    result = subprocess.run(['whoami', '/user', '/fo', 'csv', '/nh'], capture_output=True, check=True)
    sid = next((s for row in csv.reader(io.StringIO(result.stdout.decode(errors='replace'))) for s in row if re.fullmatch(r'S-1-[0-9-]+', s)), None)
    if not sid:
        fail('Cannot determine the current Windows account.')
    advapi, kernel = ctypes.WinDLL('advapi32', use_last_error=True), ctypes.WinDLL('kernel32')
    descriptor, acl = ctypes.c_void_p(), ctypes.c_void_p()
    present, defaulted = ctypes.c_int(), ctypes.c_int()
    advapi.ConvertStringSecurityDescriptorToSecurityDescriptorW.argtypes = [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.POINTER(ctypes.c_void_p), ctypes.c_void_p]
    advapi.GetSecurityDescriptorDacl.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_int), ctypes.POINTER(ctypes.c_void_p), ctypes.POINTER(ctypes.c_int)]
    advapi.SetNamedSecurityInfoW.argtypes = [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p]
    advapi.SetNamedSecurityInfoW.restype = ctypes.c_uint32
    kernel.LocalFree.argtypes = [ctypes.c_void_p]
    if not advapi.ConvertStringSecurityDescriptorToSecurityDescriptorW(f'D:P(A;OICI;FA;;;{sid})', 1, ctypes.byref(descriptor), None):
        fail('Cannot protect the backup directory.')
    try:
        if not advapi.GetSecurityDescriptorDacl(descriptor, ctypes.byref(present), ctypes.byref(acl), ctypes.byref(defaulted)) or not present.value:
            fail('Cannot read the private directory ACL.')
        if advapi.SetNamedSecurityInfoW(str(path), 1, 0x80000004, None, None, acl, None):
            fail('Cannot set the private directory ACL.')
    finally:
        kernel.LocalFree(descriptor)


def inventory(folder):
    if not folder.is_dir() or folder.is_symlink() or getattr(folder.lstat(), 'st_file_attributes', 0) & 0x400:
        fail('A real local directory is required.')
    items, total = {}, 0
    for parent, directories, files in os.walk(folder, followlinks=False):
        for name in directories:
            p = Path(parent) / name
            if p.is_symlink() or getattr(p.lstat(), 'st_file_attributes', 0) & 0x400:
                fail('Directory links are not supported.')
        for name in files:
            p = Path(parent) / name
            size = regular(p)
            total += size
            if total > LIMIT or len(items) >= MAX_FILES:
                fail('Local backup exceeds 4 GiB or 100000 files.')
            with p.open('rb') as stream:
                digest = hashlib.file_digest(stream, 'sha256').hexdigest()
            items[p.relative_to(folder).as_posix()] = {'bytes': size, 'sha256': digest}
    return items


def read_json(path):
    def pairs(values):
        result = {}
        for key, value in values:
            if key in result:
                fail('Duplicate manifest field.')
            result[key] = value
        return result
    if regular(path) > 16 * 1024**2:
        fail('Manifest is too large.')
    return json.loads(path.read_text(encoding='utf-8'), object_pairs_hook=pairs)


def allowed(name):
    if not isinstance(name, str):
        return False
    p = PurePosixPath(name)
    if '\\' in name or ':' in name or p.is_absolute() or '..' in p.parts or p.as_posix() != name or any(part.endswith(('.', ' ')) for part in p.parts):
        return False
    return name == 'database.dump' or name in {'tools/start-local-web.ps1', 'tools/parsers/markitdown_worker.py', 'tools/local_backup.py', 'docs/architecture/markitdown-requirements.lock.txt', '.local/bin/memorys-local.exe'} or name.startswith(('.local/runtime/', 'dashboard/dist/'))


def verify(folder):
    folder = Path(folder).absolute()
    data = read_json(folder / 'backup.json')
    if set(data) != {'format', 'postgres_major', 'installation', 'files', 'offline'} or data['format'] != FORMAT or data['postgres_major'] != 18 or data['offline'] is not True:
        fail('Unsupported backup manifest.')
    files = data['files']
    if not isinstance(files, dict) or not REQUIRED.issubset(files) or len(files) > MAX_FILES:
        fail('Backup is missing required files.')
    folded = set()
    for name, item in files.items():
        if not allowed(name) or name.casefold() in folded:
            fail('Unsafe or colliding backup path.')
        folded.add(name.casefold())
        if not isinstance(item, dict) or set(item) != {'bytes', 'sha256'} or type(item['bytes']) is not int or item['bytes'] < 0 or not isinstance(item['sha256'], str) or not re.fullmatch('[0-9a-f]{64}', item['sha256']):
            fail('Invalid file digest entry.')
    actual = inventory(folder)
    actual.pop('backup.json')
    if actual != files:
        fail('Backup files are missing, changed or unexpected; nothing was restored.')
    local = read_json(folder / '.local/runtime/manifest.json')
    if local.get('Format') != 'memorys.local/1' or local.get('Installation') != data['installation'] or local.get('Ready') is not True:
        fail('Backup installation binding is inconsistent.')
    return data


def connection():
    try:
        u = urlsplit(os.environ.get('MEMORYS_DATABASE_URL', ''))
        q = parse_qs(u.query, strict_parsing=True)
        user, database, port = unquote(u.username or ''), unquote(u.path.removeprefix('/')), u.port or 5432
        if u.scheme not in ('postgres', 'postgresql') or u.hostname != '127.0.0.1' or u.password is not None or u.fragment or q != {'sslmode': ['disable']} or not NAME.fullmatch(user) or not NAME.fullmatch(database) or not 1 <= port <= 65535:
            raise ValueError()
    except ValueError:
        fail('Set a password-free loopback MEMORYS_DATABASE_URL with sslmode=disable; use PGPASSWORD or pgpass.')
    # Do not inherit service, options or host overrides from another database.
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith('PG') or k.upper() in ('PGPASSWORD', 'PGPASSFILE')}
    env.update(PGHOST='127.0.0.1', PGPORT=str(port), PGUSER=user, PGDATABASE=database,
               PGSSLMODE='disable', PGCONNECT_TIMEOUT='5', PGOPTIONS='-c statement_timeout=300000 -c lock_timeout=5000')
    return env


class Postgres:
    def __init__(self, directory, env):
        self.directory, self.env = Path(directory).absolute(), env

    def tool(self, name):
        path = self.directory / (name + ('.exe' if os.name == 'nt' else ''))
        if not path.is_file():
            fail('PostgreSQL tool is missing: ' + name)
        return str(path)

    def run(self, name, *args, env=None):
        result = subprocess.run([self.tool(name), *map(str, args)], env=env or self.env, capture_output=True, timeout=300)
        if result.returncode:
            fail(name + ' failed; credentials and database output were withheld. Partial new resources were retained.')
        return result.stdout.decode('utf-8').strip()

    def version(self):
        version = self.run('psql', '-X', '-w', '-Atc', 'SHOW server_version_num')
        if not version.isdigit() or int(version) // 10000 != 18:
            fail('This local backup format currently requires PostgreSQL 18 on both ends.')

    @contextlib.contextmanager
    def frozen(self):
        # SHARE locks block platform writes while allowing pg_dump ACCESS SHARE.
        # Never run schema/table names supplied by a backup or CLI argument.
        sql = r"""BEGIN;
SET LOCAL idle_in_transaction_session_timeout='600s';
SELECT 'LOCK TABLE ' || string_agg(format('%I.%I',n.nspname,c.relname),',') || ' IN SHARE MODE'
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE c.relkind='r' AND (n.nspname='akos_withdraw_v1' OR (n.nspname='public' AND c.relname='memorys_schema_migrations'))
\gexec
SELECT 'memorys-backup-locked';
"""
        proc = subprocess.Popen([self.tool('psql'), '-X', '-w', '-qAt', '-v', 'ON_ERROR_STOP=1', '-f', '-'], env=self.env,
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, bufsize=1)
        line = []
        def read():
            line.append(proc.stdout.readline().strip())
        try:
            proc.stdin.write(sql)
            proc.stdin.flush()
            reader = threading.Thread(target=read, daemon=True)
            reader.start()
            reader.join(10)
            if reader.is_alive() or line != ['memorys-backup-locked'] or proc.poll() is not None:
                fail('Cannot freeze platform writes; stop services and other writers before backup.')
            yield proc
            if proc.poll() is not None:
                fail('Backup write barrier was lost; backup is incomplete.')
        finally:
            if proc.poll() is None:
                proc.terminate()
            proc.wait(timeout=10)
            proc.stdin.close()
            proc.stdout.close()


def copy_file(source, target, expected):
    regular(source)
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with source.open('rb') as src, target.open('xb') as dst:
        shutil.copyfileobj(src, dst)
    target.chmod(0o600)
    with target.open('rb') as stream:
        got = {'bytes': target.stat().st_size, 'sha256': hashlib.file_digest(stream, 'sha256').hexdigest()}
    if got != expected:
        fail('A file changed while copying; incomplete destination retained.')


def doctor(binary, state, env, cwd):
    result = subprocess.run([str(binary), 'doctor', '--state', str(state)], cwd=cwd, env=env, capture_output=True, timeout=60)
    if result.returncode:
        fail('Runtime/database binding check failed; run memorys-local doctor separately.')
    checks = json.loads(result.stdout)['checks']
    if any(checks.get(name) is not True for name in ('database', 'schema', 'identity', 'permissions', 'configuration', 'local_state', 'signing_key_file')):
        fail('Runtime/database identity or configuration is inconsistent.')


def backup(destination, pg_bin, workspace=ROOT, state=None, offline=False):
    if not offline:
        fail('Stop services and other local writers, then pass --offline-confirmed.')
    workspace = Path(workspace).absolute()
    state = Path(state or workspace / '.local/runtime').absolute()
    destination = Path(destination).absolute()
    if destination.is_relative_to(state) or state.is_relative_to(destination):
        fail('Backup directory must be separate from the runtime directory.')
    env, sources = connection(), {}
    pg = Postgres(pg_bin, env)
    pg.version()
    before = inventory(state)
    for name, value in before.items():
        sources['.local/runtime/' + name] = (state / name, value)
    for name in ('.local/bin/memorys-local.exe', 'tools/start-local-web.ps1', 'tools/parsers/markitdown_worker.py',
                 'tools/local_backup.py', 'docs/architecture/markitdown-requirements.lock.txt'):
        path = workspace / name
        size = regular(path)
        sources[name] = (path, {'bytes': size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
    assets = inventory(workspace / 'dashboard/dist')
    if 'index.html' not in assets or not CORE.issubset(sources):
        fail('Build the local executable/dashboard and initialize first.')
    for name, value in assets.items():
        sources['dashboard/dist/' + name] = (workspace / 'dashboard/dist' / name, value)
    doctor(workspace / '.local/bin/memorys-local.exe', state, env, workspace)
    local = read_json(state / 'manifest.json')
    secure_directory(destination)
    with pg.frozen():
        doctor(workspace / '.local/bin/memorys-local.exe', state, env, workspace)
        pg.run('pg_dump', '-w', '-Fc', '-f', destination / 'database.dump')
        for name, (source, value) in sources.items():
            copy_file(source, destination / name, value)
        if inventory(state) != before:
            fail('Runtime changed during backup; stop all writers and retry in a new directory.')
    files = inventory(destination)
    if not all(allowed(name) for name in files):
        fail('Unsupported backup file path.')
    data = {'format': FORMAT, 'postgres_major': 18, 'installation': local['Installation'], 'offline': True, 'files': files}
    with (destination / 'backup.json').open('x', encoding='utf-8') as stream:
        json.dump(data, stream, indent=2)
    verify(destination)
    return {'state': 'backed-up', 'files': len(files), 'production_acceptance': False}


def restore(source, destination, database, pg_bin):
    if not NAME.fullmatch(database) or database.lower() in ('postgres', 'template0', 'template1'):
        fail('Choose a new application database name, not a system database.')
    data = verify(source)
    env = connection()
    pg = Postgres(pg_bin, env)
    pg.version()
    # Restoring means executing a trusted SQL archive; hashes detect corruption,
    # not authenticity. Only restore your own protected backup.
    pg.run('pg_restore', '--list', Path(source) / 'database.dump')
    destination = Path(destination).absolute()
    secure_directory(destination)
    for name, item in data['files'].items():
        copy_file(Path(source) / name, destination / name, item)
    new_env = dict(env, PGDATABASE=database)
    maintenance = dict(env, PGDATABASE='postgres')
    pg.run('createdb', '-w', '--maintenance-db=postgres', '--template=template0', '--encoding=UTF8', database, env=maintenance)
    pg.run('pg_restore', '-w', '--exit-on-error', '--no-owner', '--no-acl', '-d', database, destination / 'database.dump', env=new_env)
    new_env['MEMORYS_DATABASE_URL'] = f"postgres://{env['PGUSER']}@127.0.0.1:{env['PGPORT']}/{database}?sslmode=disable"
    doctor(destination / '.local/bin/memorys-local.exe', destination / '.local/runtime', new_env, destination)
    return {'state': 'restored', 'database': database, 'production_acceptance': False,
            'next': 'Set MEMORYS_DATABASE_URL to the new database; recreate the parser environment and pinned local model before AI use.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    create = sub.add_parser('backup')
    create.add_argument('destination', type=Path)
    create.add_argument('--pg-bin', required=True, type=Path)
    create.add_argument('--state', type=Path)
    create.add_argument('--offline-confirmed', action='store_true')
    check = sub.add_parser('check')
    check.add_argument('source', type=Path)
    load = sub.add_parser('restore')
    load.add_argument('source', type=Path)
    load.add_argument('destination', type=Path)
    load.add_argument('--database', required=True)
    load.add_argument('--pg-bin', required=True, type=Path)
    args = parser.parse_args()
    try:
        if args.command == 'backup':
            result = backup(args.destination, args.pg_bin, state=args.state, offline=args.offline_confirmed)
        elif args.command == 'restore':
            result = restore(args.source, args.destination, args.database, args.pg_bin)
        else:
            data = verify(args.source)
            result = {'state': 'verified', 'files': len(data['files']), 'production_acceptance': False}
        print(json.dumps(result))
        return 0
    except BackupError as error:
        print(str(error), file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print('Local backup operation failed; no existing database or destination was overwritten. Incomplete new resources may remain.', file=sys.stderr)
    return 1


if __name__ == '__main__':
    sys.exit(main())

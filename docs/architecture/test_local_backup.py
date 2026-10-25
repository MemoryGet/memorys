"""Corruption, path safety, credential isolation and no-overwrite backup checks."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

MODULE = Path(__file__).resolve().parents[2] / 'tools/local_backup.py'
spec = importlib.util.spec_from_file_location('local_backup', MODULE)
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)


class LocalBackupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.bundle = self.root / 'bundle'
        self.bundle.mkdir()
        for name in backup.REQUIRED:
            p = self.bundle / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(b'opaque synthetic fixture')
        (self.bundle / '.local/runtime/manifest.json').write_text(json.dumps({'Format': 'memorys.local/1', 'Installation': 'synthetic', 'Ready': True}))
        self.data = {'format': backup.FORMAT, 'postgres_major': 18, 'installation': 'synthetic', 'offline': True, 'files': backup.inventory(self.bundle)}
        self.save()

    def tearDown(self):
        self.temp.cleanup()

    def save(self):
        (self.bundle / 'backup.json').write_text(json.dumps(self.data), encoding='utf-8')

    def test_verified_without_database_or_credentials(self):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual('synthetic', backup.verify(self.bundle)['installation'])

    def test_changed_private_key_rejected(self):
        (self.bundle / '.local/runtime/private.pem').write_bytes(b'changed')
        with self.assertRaises(backup.BackupError):
            backup.verify(self.bundle)

    def test_missing_original_and_extra_file_rejected(self):
        for action in ('missing', 'extra'):
            with self.subTest(action=action):
                if action == 'missing':
                    (self.bundle / 'database.dump').unlink()
                else:
                    (self.bundle / 'database.dump').write_bytes(b'opaque synthetic fixture')
                    (self.bundle / 'unexpected.txt').write_text('extra')
                with self.assertRaises(backup.BackupError):
                    backup.verify(self.bundle)

    def test_required_application_file_cannot_be_omitted_from_manifest(self):
        name = 'tools/start-local-web.ps1'
        (self.bundle / name).unlink()
        del self.data['files'][name]
        self.save()
        with self.assertRaises(backup.BackupError):
            backup.verify(self.bundle)

    def test_path_escape_windows_aliases_and_case_collision_rejected(self):
        for name in ('../private.pem', '/private.pem', '.local/runtime/../x', r'.local\runtime\x', '.local/runtime/x:stream', '.local/runtime/x.', '.local/runtime/x ', '.local/runtime/PRIVATE.pem'):
            with self.subTest(name=name):
                self.data['files'][name] = {'bytes': 0, 'sha256': '0' * 64}
                self.save()
                with self.assertRaises(backup.BackupError):
                    backup.verify(self.bundle)
                del self.data['files'][name]

    def test_duplicate_manifest_and_installation_mismatch_rejected(self):
        (self.bundle / 'backup.json').write_text('{"format":"x","format":"x"}')
        with self.assertRaises(backup.BackupError):
            backup.verify(self.bundle)
        self.data['installation'] = 'another-installation'
        self.save()
        with self.assertRaises(backup.BackupError):
            backup.verify(self.bundle)

    def test_hardlinked_source_is_rejected(self):
        p = self.bundle / 'database.dump'
        os.link(p, self.root / 'other')
        with self.assertRaises(backup.BackupError):
            backup.verify(self.bundle)

    def test_copy_change_does_not_overwrite_existing_destination(self):
        target = self.root / 'existing'
        target.write_bytes(b'keep')
        with self.assertRaises(FileExistsError):
            backup.copy_file(self.bundle / 'database.dump', target, {})
        self.assertEqual(b'keep', target.read_bytes())

    def test_existing_directory_rejected_before_acl_changes(self):
        with self.assertRaises(FileExistsError):
            backup.secure_directory(self.bundle)
        self.assertEqual('synthetic', backup.verify(self.bundle)['installation'])

    def test_explicit_offline_ack_required_before_connect(self):
        with patch.object(backup, 'connection', side_effect=AssertionError('database touched')):
            with self.assertRaises(backup.BackupError):
                backup.backup(self.root / 'new', self.root)

    def test_system_database_restore_rejected_before_connect(self):
        for name in ('postgres', 'template0', 'template1', 'bad-name', 'foo;DROP DATABASE postgres'):
            with self.subTest(name=name), patch.object(backup, 'connection', side_effect=AssertionError('database touched')):
                with self.assertRaises(backup.BackupError):
                    backup.restore(self.bundle, self.root / 'new', name, self.root)

    def test_connection_uses_password_environment_and_discards_overrides(self):
        values = {'MEMORYS_DATABASE_URL': 'postgres://postgres@127.0.0.1:55443/source?sslmode=disable', 'PGPASSWORD': 'synthetic-secret', 'PGHOSTADDR': '192.0.2.1', 'PGSERVICE': 'other', 'PGOPTIONS': '-c evil=on'}
        with patch.dict(os.environ, values, clear=True):
            env = backup.connection()
        self.assertEqual('127.0.0.1', env['PGHOST'])
        self.assertEqual('source', env['PGDATABASE'])
        self.assertEqual('synthetic-secret', env['PGPASSWORD'])
        self.assertNotIn('PGHOSTADDR', env)
        self.assertNotIn('PGSERVICE', env)
        self.assertNotIn('evil', env['PGOPTIONS'])

    def test_remote_password_url_and_query_overrides_rejected(self):
        for url in ('postgres://postgres:secret@127.0.0.1/source?sslmode=disable', 'postgres://postgres@remote/source?sslmode=disable', 'postgres://postgres@127.0.0.1/source?sslmode=disable&hostaddr=remote'):
            with self.subTest(url=url), patch.dict(os.environ, {'MEMORYS_DATABASE_URL': url}, clear=True):
                with self.assertRaises(backup.BackupError) as error:
                    backup.connection()
                self.assertNotIn(url, str(error.exception))


if __name__ == '__main__':
    unittest.main()

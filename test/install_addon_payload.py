"""Check the signed payload and reversible swaps at the filesystem boundary."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import warnings
import zipfile

script = Path(__file__).resolve().parents[1] / 'tools/install-local-macos.py'
spec = importlib.util.spec_from_file_location('installer', script)
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class PayloadTests(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory()
        self.addCleanup(scratch.cleanup)
        self.root = Path(scratch.name)
        self.app = self.root / 'Looking for Guidance.app'
        self.resources = self.app / 'Contents/Resources'
        self.resources.mkdir(parents=True)
        self.data = {'looking-for-guidance.toc': b'Version: demo', 'Media/Emoji/atlas.tga': b'pixels'}
        self.manifest = {name: hashlib.sha256(value).hexdigest() for name, value in self.data.items()}
        self.manifest_path = self.resources / 'addon-files.json'
        self.manifest_path.write_text(json.dumps(self.manifest))
        self.output = self.root / 'staged'
        self.zip_path = self.resources / 'addon-payload.zip'

    def archive(self, items):
        with zipfile.ZipFile(self.zip_path, 'w') as archive:
            for name, value in items:
                archive.writestr(name, value)

    def test_exact_signed_payload(self):
        self.archive(self.data.items())
        installer.stage_addon(self.app, self.output, installer.load_manifest(self.app))
        self.assertEqual((self.output / 'Media/Emoji/atlas.tga').read_bytes(), b'pixels')

    def test_unsafe_names_and_collisions(self):
        for name in ('../x', 'a//b', 'x/./b', '._metadata', 'media/emoji/atlas.TGA', '/tmp/out', 'dir\\file'):
            with self.subTest(name=name):
                self.manifest_path.write_text(json.dumps({**self.manifest, name: 'f'*64}))
                with self.assertRaises(ValueError):
                    installer.load_manifest(self.app)

    def test_rejects_duplicate_missing_unexpected_symlink_and_digest_mismatch(self):
        genuine = list(self.data.items())
        link = zipfile.ZipInfo('Media/Emoji/atlas.tga')
        link.create_system = 3
        link.external_attr = 0o120777 << 16
        for label, items in [
            ('duplicate', genuine + [genuine[0]]),
            ('missing', genuine[:1]),
            ('unexpected', genuine + [('../outside', b'escape')]),
            ('link', genuine[:1] + [(link, b'/tmp/x')]),
            ('corrupt', genuine[:1] + [(genuine[1][0], b'wrong')]),
        ]:
            with self.subTest(label=label), warnings.catch_warnings():
                warnings.simplefilter('ignore', UserWarning)
                self.archive(items)
                with self.assertRaises(ValueError):
                    installer.stage_addon(self.app, self.root/label, self.manifest)
        self.assertFalse((self.root / 'outside').exists())

    def test_rolls_back_app_and_addon_at_each_commit_step(self):
        original_rename = os.rename
        for failure in ('move_old_app', 'move_new_app', 'move_old_addon', 'move_new_addon', 'readback'):
            with self.subTest(failure=failure):
                base = self.root / failure
                base.mkdir()
                installed, next_app, backup, addon, new, previous = (base/x for x in
                    ('app', 'next-app', 'backup-app', 'addon', 'next-addon', 'previous-addon'))
                for path, value in ((installed,'old'), (next_app,'new'), (addon,'old'), (new,'new')):
                    path.mkdir()
                    (path/'version').write_text(value)
                fail_src = dict(zip(('move_old_app','move_new_app','move_old_addon','move_new_addon'),
                                    (installed, next_app, addon, new))).get(failure)
                def injected(source, target):
                    nonlocal fail_src
                    if source == fail_src:
                        fail_src = None
                        raise RuntimeError('interrupted rename')
                    original_rename(source, target)
                with mock.patch.object(installer.os, 'rename', injected), self.assertRaises((RuntimeError, ValueError)):
                    installer.replace_pair(installed, next_app, backup, addon, new, previous, {'version':'f'*64})
                self.assertEqual((installed/'version').read_text(), 'old')
                self.assertEqual((addon/'version').read_text(), 'old')

    def test_archives_only_failed_receipt_without_worker(self):
        pending, backups = self.root/'pending', self.root/'backups'
        pending.mkdir()
        backups.mkdir()
        log = pending/'install.log'
        log.write_text('Installing\n')
        with self.assertRaises(SystemExit):
            installer.archive_failed_update(pending, backups)
        failure = 'Новое приложение установлено, но файлы WoW не подтверждены. Проверь доступ Companion к тому игры.\n'
        log.write_text(failure)
        with mock.patch.object(installer.subprocess, 'check_output', return_value='/sample/tools/install-local-macos.py --apply-pending'), self.assertRaises(SystemExit):
            installer.archive_failed_update(pending, backups)
        self.assertTrue(log.is_file())
        with mock.patch.object(installer.subprocess, 'check_output', return_value=''):
            installer.archive_failed_update(pending, backups)
        self.assertFalse(pending.exists())
        saved, = backups.glob('previous-pending-*/pending-local-update/install.log')
        self.assertEqual(saved.read_text(), failure)


if __name__ == '__main__':
    unittest.main()

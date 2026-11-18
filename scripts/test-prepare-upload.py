#!/usr/bin/env python3
"""Regression tests for the local source-publication boundary; no network calls."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location('prepare_upload', Path(__file__).with_name('prepare-upload.py'))
prepare_upload = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare_upload)


class PrepareUploadTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='artex-upload-test-')
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.source = self.base / 'source'
        self.source.mkdir()

    def write(self, relative, data=b'ordinary source\n'):
        path = self.source / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def prepare(self, destination=None):
        destination = destination or self.base / 'review'
        with contextlib.redirect_stdout(io.StringIO()):
            manifest = prepare_upload.prepare(self.source, destination)
        return destination, manifest

    def test_exclusions_manifest_modes_and_deterministic_zip(self):
        self.write('README.md', b'# English source\n')
        self.write('artex.go', b'package main\n')
        self.write('cmd/artex/main.go', b'package main\n')
        self.write('cmd/tool-worker/main.go', b'package main\n')
        self.write('cmd/worker-broker/main.go', b'package main\n')
        self.write('.env.example', b'API_KEY=\n')
        executable = self.write('scripts/start.sh', b'#!/bin/sh\nexit 0\n')
        executable.chmod(0o750)
        private_paths = (
            'SOURCE-MANIFEST.json', 'config.json', 'worker-policy.json', '.env',
            '.npmrc', '.pypirc', '.netrc', '.git-credentials', 'credentials.json',
            '.aws/credentials', '.ssh/id_ed25519', '.gnupg/private-keys-v1.d/key',
            '.docker/config.json', '.kube/config', '.config/gcloud/tokens.json',
            'node_modules/pkg/index.js', '.swc-cache/bin', '.cache/item', '.turbo/log',
            'web/out/index.html', 'server/webui/dist/index.html', 'data/record.txt',
            'tool-worker', 'worker-broker', 'artex', 'artex.old-1', 'tool-worker.exe',
            'worker-broker.new-1', 'native.o', 'library.so',
        )
        for relative in private_paths:
            self.write(relative, b'excluded local file\n')
        self.write('renamed-native', b'\x7fELF' + b'\0' * 32)
        destination, manifest = self.prepare(self.base / 'one' / 'review')
        paths = {entry['path'] for entry in manifest}
        self.assertEqual(paths, {'README.md', 'artex.go', '.env.example', 'scripts/start.sh',
                                 'cmd/artex/main.go', 'cmd/tool-worker/main.go', 'cmd/worker-broker/main.go'})
        self.assertEqual({entry['path'] for entry in json.loads((destination / 'SOURCE-MANIFEST.json').read_text())['files']}, paths)
        for entry in manifest:
            copied = destination / entry['path']
            self.assertEqual(hashlib.sha256(copied.read_bytes()).hexdigest(), entry['sha256'])
            self.assertEqual(stat.S_IMODE(copied.stat().st_mode), int(entry['mode'], 8))
        archive = destination.with_suffix('.zip')
        with zipfile.ZipFile(archive) as zipped:
            self.assertEqual(len(zipped.namelist()), len(manifest) + 1)
            for item in zipped.infolist():
                self.assertEqual(item.date_time, prepare_upload.ZIP_TIMESTAMP)
                self.assertEqual(zipped.read(item), (destination / Path(item.filename).relative_to('review')).read_bytes())
        other, _ = self.prepare(self.base / 'two' / 'review')
        self.assertEqual(archive.read_bytes(), other.with_suffix('.zip').read_bytes())
        self.assertTrue((self.source / 'SOURCE-MANIFEST.json').exists())

    def test_private_key_and_token_signatures_abort_before_output_and_redact(self):
        markers = [
            b'-' * 5 + b'BEGIN ' + kind + b'-----\nnot-a-real-key\n'
            for kind in (b'PRIVATE KEY', b'ENCRYPTED PRIVATE KEY', b'DSA PRIVATE KEY',
                         b'OPENSSH PRIVATE KEY', b'PGP PRIVATE KEY BLOCK')
        ] + [b'ghp_' + b'x' * 36, b'github_pat_' + b'x' * 80]
        for marker in markers:
            with self.subTest(marker_type=marker[:20]):
                self.write('notes.txt', marker)
                destination = self.base / 'review'
                with self.assertRaises(SystemExit) as raised:
                    self.prepare(destination)
                self.assertIn('value redacted', str(raised.exception))
                self.assertNotIn('not-a-real-key', str(raised.exception))
                self.assertFalse(destination.exists())
                self.assertFalse(destination.with_suffix('.zip').exists())

    def test_symlinks_and_special_files_are_rejected(self):
        outside = self.base / 'outside'
        outside.mkdir()
        (outside / 'secret.txt').write_text('private test fixture')
        for target in (outside, outside / 'secret.txt'):
            link = self.source / 'linked'
            link.symlink_to(target)
            with self.assertRaises(SystemExit):
                self.prepare()
            link.unlink()
        fifo = self.source / 'pipe'
        os.mkfifo(fifo)
        with self.assertRaises(SystemExit):
            self.prepare()

    def test_file_swapped_for_symlink_between_stat_and_open_is_rejected(self):
        self.write('notes.txt')
        outside = self.base / 'outside-secret.txt'
        outside.write_text('private test fixture')
        real_open = os.open

        def replace_before_open(path, flags, *args, **kwargs):
            if path == 'notes.txt':
                (self.source / path).unlink()
                (self.source / path).symlink_to(outside)
            return real_open(path, flags, *args, **kwargs)

        with patch.object(prepare_upload.os, 'open', replace_before_open):
            with self.assertRaisesRegex(SystemExit, 'safely opened'):
                self.prepare()
        self.assertFalse((self.base / 'review').exists())

    def test_preexisting_output_paths_are_untouched_and_fail_before_read(self):
        for occupied in ('review', 'review.zip', 'review.zip.sha256'):
            with self.subTest(path=occupied):
                parent = self.base / occupied.replace('.', '-')
                parent.mkdir()
                path = parent / occupied
                path.write_text('keep this file')
                with patch.object(prepare_upload, 'read_source', side_effect=AssertionError('must preflight before reading')):
                    with self.assertRaisesRegex(SystemExit, 'already exists'):
                        self.prepare(parent / 'review')
                self.assertEqual(path.read_text(), 'keep this file')

    def test_dangling_output_symlinks_and_inside_source_are_rejected(self):
        destination = self.base / 'review'
        destination.with_suffix('.zip').symlink_to(self.base / 'not-created')
        with self.assertRaisesRegex(SystemExit, 'already exists'):
            self.prepare(destination)
        self.assertTrue(destination.with_suffix('.zip').is_symlink())
        with self.assertRaisesRegex(SystemExit, 'outside the source tree'):
            self.prepare(self.source / 'nested-review')


if __name__ == '__main__':
    unittest.main()

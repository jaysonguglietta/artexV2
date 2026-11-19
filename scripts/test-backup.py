#!/usr/bin/env python3
"""Local cryptographic/archive/failure regressions; never touches Docker or a DB."""
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import struct
import sys
import tarfile
import time
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
spec = importlib.util.spec_from_file_location('artex_backup', Path(__file__).with_name('artex-backup.py'))
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)


@unittest.skipUnless(shutil.which('openssl'), 'OpenSSL3+ required')
class BackupTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='artex-backup-unit-')
        cls.root = Path(cls.temporary.name)
        for role in ['recipient', 'signer', 'attacker']:
            backup.execute(['openssl', 'req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '2',
                            '-subj', '/CN=Same subject deliberately', '-sha256',
                            '-keyout', str(cls.root / (role + '.key')), '-out', str(cls.root / (role + '.crt'))])
            (cls.root / (role + '.key')).chmod(0o600)

    @classmethod
    def tearDownClass(cls): cls.temporary.cleanup()

    def setUp(self):
        self.directory = Path(tempfile.mkdtemp(dir=self.root))
        self.snapshot = self.directory / 'snapshot'
        self.snapshot.mkdir()
        for name, value in {'postgres.dump': b'synthetic dump', 'files/private/jwt.key': b'x'*32,
                            'config/.env': b'SYNTHETIC=only', 'config/runtime-containers.json': b'{}', 'files/artifacts/manifest.json': b'ordinary user artifact'}.items():
            path = self.snapshot / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(value)
        self.manifest = {'version': 1, 'signer_certificate_sha256': backup.certificate_pin(self.root / 'signer.crt'),
                         'files': {path.relative_to(self.snapshot).as_posix(): backup.digest_file(path)
                                   for path in self.snapshot.rglob('*') if path.is_file()}}
        self.write_manifest()

    def write_manifest(self): (self.snapshot / 'manifest.json').write_text(json.dumps(self.manifest))

    def encrypt(self, signer='signer'):
        output = self.directory / ('backup-' + signer + '.cms')
        backup.sign_encrypt(self.snapshot, output, self.root / 'recipient.crt',
                            self.root / (signer + '.crt'), self.root / (signer + '.key'), 1 << 20)
        return output

    def extract(self, encrypted, recipient='recipient', signer='signer'):
        output = self.directory / ('verified-' + str(len(list(self.directory.iterdir()))))
        output.mkdir()
        return backup.authenticated_extract(encrypted, self.root / (recipient + '.key'), self.root / (signer + '.crt'), output, 1 << 20)

    def test_signed_encrypted_roundtrip_and_artifact_named_manifest(self):
        encrypted = self.encrypt()
        self.assertNotIn(b'synthetic dump', encrypted.read_bytes())
        self.assertEqual(self.extract(encrypted), self.manifest)
        self.assertEqual(encrypted.stat().st_mode & 0o777, 0o600)

    def test_ciphertext_corruption_is_refused_before_archive_parse(self):
        encrypted = self.encrypt()
        data = bytearray(encrypted.read_bytes())
        data[len(data)//2] ^= 1
        encrypted.write_bytes(data)
        with mock.patch.object(backup, 'extract_tar', side_effect=AssertionError('parsed unauthenticated data')) as parser:
            with self.assertRaises((ValueError, RuntimeError)): self.extract(encrypted)
            parser.assert_not_called()

    def test_wrong_decryption_identity_refused(self):
        with self.assertRaises(RuntimeError): self.extract(self.encrypt(), recipient='attacker')

    def test_public_recipient_cannot_forge_backup_origin(self):
        # Anyone can encrypt with the public recipient; only the separately
        # pinned signer identity authorizes a backup for restoration.
        forged = self.encrypt(signer='attacker')
        with mock.patch.object(backup, 'extract_tar') as parser:
            with self.assertRaises(RuntimeError): self.extract(forged)
            parser.assert_not_called()

    def test_embedded_signer_certificate_cannot_replace_independent_pin(self):
        with self.assertRaises(RuntimeError): self.extract(self.encrypt(), signer='attacker')

    def test_trusted_certificate_bundle_is_refused_before_archive_parse(self):
        encrypted = self.encrypt()
        bundle = self.directory / 'trusted-bundle.pem'
        bundle.write_bytes((self.root / 'signer.crt').read_bytes() + (self.root / 'attacker.crt').read_bytes())
        output = self.directory / 'bundle-output'
        output.mkdir()
        with mock.patch.object(backup, 'extract_tar') as parser:
            with self.assertRaises(ValueError):
                backup.authenticated_extract(encrypted, self.root / 'recipient.key', bundle, output, 1 << 20)
            parser.assert_not_called()

    def test_authenticated_file_manifest_mismatch_is_refused(self):
        self.manifest['files']['postgres.dump']['sha256'] = '0' * 64
        self.write_manifest()
        with self.assertRaises(ValueError): self.extract(self.encrypt())

    def test_archive_traversal_links_special_files_and_duplicates_refused(self):
        for name, kind in [('../outside', tarfile.REGTYPE), ('/absolute', tarfile.REGTYPE),
                           ('link', tarfile.SYMTYPE), ('hard', tarfile.LNKTYPE), ('device', tarfile.CHRTYPE)]:
            with self.subTest(name=name, kind=kind):
                data = io.BytesIO()
                with tarfile.open(fileobj=data, mode='w') as archive:
                    member = tarfile.TarInfo(name)
                    member.type = kind
                    member.linkname = '../outside'
                    archive.addfile(member)
                data.seek(0)
                with self.assertRaises(ValueError): backup.extract_tar(data, self.directory / 'bad', 100)
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w') as archive:
            for _ in range(2): archive.addfile(tarfile.TarInfo('duplicate'))
        data.seek(0)
        with self.assertRaises(ValueError): backup.extract_tar(data, self.directory / 'duplicates', 100)

    def test_archive_byte_budget_rejects_before_member_write(self):
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w') as archive:
            member = tarfile.TarInfo('oversized')
            member.size = 1000
            archive.addfile(member, io.BytesIO(b'x'*1000))
        data.seek(0)
        with self.assertRaises(ValueError): backup.extract_tar(data, self.directory / 'oversized', 10)
        self.assertFalse((self.directory / 'oversized/oversized').exists())

    def test_chunk_roundtrip_and_reordering_truncation_or_trailing_data_refused(self):
        # A small test chunk exercises the same fixed framing without allocating
        # hundreds of megabytes per negative; production uses8MiB.
        with mock.patch.object(backup, 'CHUNK_BYTES', 1024):
            encrypted = self.encrypt()
            self.assertEqual(self.extract(encrypted), self.manifest)
            data = encrypted.read_bytes()
            offset = len(backup.CHUNKED)
            chunks = []
            while offset < len(data):
                size = struct.unpack('>I', data[offset:offset+4])[0]
                chunks.append(data[offset:offset+4+size])
                offset += 4 + size
            self.assertGreater(len(chunks), 2)
            cases = [chunks[1:2] + chunks[:1] + chunks[2:], chunks[:-1], chunks + chunks[:1]]
            for index, reordered in enumerate(cases):
                corrupt = self.directory / ('chunk-negative-' + str(index))
                corrupt.write_bytes(backup.CHUNKED + b''.join(reordered))
                with mock.patch.object(backup, 'extract_tar') as parser:
                    with self.assertRaises(ValueError): self.extract(corrupt)
                    parser.assert_not_called()

    def test_backup_fifo_is_refused_without_blocking(self):
        if not hasattr(os, 'mkfifo'): self.skipTest('POSIX FIFO required')
        fifo = self.directory / 'not-a-backup'
        os.mkfifo(fifo, 0o600)
        started = time.monotonic()
        with self.assertRaises(ValueError): backup.open_backup(fifo, 1 << 20)
        self.assertLess(time.monotonic() - started, 1)

    def test_oversized_chunk_refused_before_crypto_allocation(self):
        corrupt = self.directory / 'oversized-chunk'
        corrupt.write_bytes(backup.CHUNKED + struct.pack('>I', backup.CHUNK_BYTES + backup.CMS_OVERHEAD + 1))
        with mock.patch.object(backup, 'certificate_pin', return_value='0'*64), mock.patch.object(backup, 'execute') as crypto:
            with self.assertRaises(ValueError): self.extract(corrupt)
            crypto.assert_not_called()

    def test_private_identity_permissions_are_enforced(self):
        key = self.directory / 'exposed.key'
        key.write_bytes(b'not an actual identity')
        key.chmod(0o644)
        with self.assertRaises(ValueError): backup.private_file(key, secret=True)

    def test_blocked_row_stream_has_active_deadline_and_restores_producers(self):
        api = 'a'*64
        calls = []
        def fake(arguments, **options):
            calls.append(arguments)
            if arguments[:1] == ['inspect']:
                return json.dumps([{'Config': {'Labels': {'com.docker.compose.project': 'fixture'}}}]).encode()
            if arguments[:1] == ['ps']:
                return (api + '\n').encode() if arguments[-1].endswith('=artex') else b''
            return b''
        started = time.monotonic()
        with mock.patch.object(backup, 'docker', side_effect=fake):
            with self.assertRaises(RuntimeError):
                with backup.quiesce([], api):
                    backup.stream_command([sys.executable, '-c', 'import time; print("row",flush=True); time.sleep(60)'],
                                          backup.RowDigest(), 1024, timeout=.1)
        self.assertLess(time.monotonic() - started, 3)
        self.assertEqual(calls[-1], ['start', api])

    def test_failed_snapshot_restarts_exact_previously_running_container_ids(self):
        api, broker = 'a'*64, 'b'*64
        calls = []
        def fake(arguments, **options):
            calls.append(arguments)
            if arguments[:1] == ['inspect']:
                return json.dumps([{'Config': {'Labels': {'com.docker.compose.project': 'fixture'}}}]).encode()
            if arguments[:1] == ['ps']:
                return ((api if arguments[-1].endswith('=artex') else broker) + '\n').encode()
            return b''
        with mock.patch.object(backup, 'docker', side_effect=fake):
            with self.assertRaisesRegex(RuntimeError, 'snapshot failed'):
                with backup.quiesce([], api): raise RuntimeError('snapshot failed')
        self.assertIn(['stop', '--time', '120', api, broker], calls)
        self.assertEqual(calls[-1], ['start', api, broker])


if __name__ == '__main__': unittest.main()

#!/usr/bin/env python3
"""CMS profile regressions using real OpenSSL outputs and hostile DER fixtures."""
from pathlib import Path
import importlib.util
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
from backup_crypto import MAX_CHUNK_BYTES, validate_cms_chunk


def tlv(tag, value):
    count = (len(value).bit_length() + 7) // 8
    length = bytes([len(value)]) if len(value) < 128 else bytes([128 + count]) + len(value).to_bytes(count, 'big')
    return bytes([tag]) + length + value


def sequence(*values): return tlv(0x30, b''.join(values))
def oid(value): return tlv(0x06, bytes.fromhex(value))


def profile(*, nonce=b'n'*12, mac=b'm'*16, content=b'c'*9, recipients=1,
            attributes=b'', name_count=1, sha_null=False, label=None):
    """Structural fixture only: fake ciphertext must never be decrypted."""
    sha = sequence(oid('608648016503040201'), *([tlv(0x05, b'')] if sha_null else []))
    oaep = [tlv(0xA0, sha), tlv(0xA1, sequence(oid('2a864886f70d010108'), sha))]
    if label is not None:
        oaep.append(tlv(0xA2, sequence(oid('2a864886f70d010109'), tlv(0x04, label))))
    name = sequence(*(tlv(0x31, sequence(oid('550403'), tlv(0x0C, b'fixture'))) for _ in range(name_count)))
    recipient = sequence(tlv(0x02, b'\0'), sequence(name, tlv(0x02, b'\1')),
                         sequence(oid('2a864886f70d010107'), sequence(*oaep)), tlv(0x04, b'k'*384))
    encrypted = sequence(oid('2a864886f70d010701'),
                         sequence(oid('60864801650304012e'), sequence(tlv(0x04, nonce), tlv(0x02, b'\x10'))),
                         tlv(0x80, content))
    envelope = sequence(tlv(0x02, b'\0'), tlv(0x31, recipient * recipients), encrypted,
                        tlv(0x04, mac), attributes)
    return sequence(oid('2a864886f70d0109100117'), tlv(0xA0, envelope))


class DERProfileTests(unittest.TestCase):
    def assert_rejected(self, data):
        with self.assertRaisesRegex(ValueError, 'authenticated encryption profile'):
            validate_cms_chunk(data)

    def test_structural_profile_allows_absent_null_and_empty_oaep_label(self):
        for options in [{}, {'sha_null': True}, {'label': b''}]:
            with self.subTest(options=options): validate_cms_chunk(profile(**options))

    def test_strict_aead_sizes_and_one_recipient(self):
        for options in [{'nonce': b'n'*11}, {'nonce': b'n'*13}, {'mac': b'm'*12},
                        {'mac': b'm'*17}, {'content': b'c'*8}, {'content': b'c'*((8 << 20) + 10)},
                        {'recipients': 0}, {'recipients': 2}, {'label': b'unapproved label'},
                        {'attributes': tlv(0xA1, b'')}, {'attributes': tlv(0xA2, b'')}]:
            with self.subTest(options=list(options)): self.assert_rejected(profile(**options))

    def test_no_algorithm_or_hash_substitution(self):
        good = profile()
        for old, new in [('2a864886f70d0109100117', '2a864886f70d0109100118'),
                         ('2a864886f70d010107', '2a864886f70d010101'),
                         ('2a864886f70d010108', '2a864886f70d010109'),
                         ('608648016503040201', '608648016503040202'),
                         ('60864801650304012e', '60864801650304012a')]:
            with self.subTest(oid=old): self.assert_rejected(good.replace(bytes.fromhex(old), bytes.fromhex(new)))

    def test_no_ber_noncanonical_lengths_truncation_or_trailing_data(self):
        good = profile()
        count = good[1] & 127
        body = good[2 + count:]
        self.assert_rejected(b'\x30\x80' + body + b'\0\0')
        self.assert_rejected(b'\x30' + bytes([0x80 + count + 1]) + b'\0' + good[2:])
        self.assert_rejected(good + b'\0')
        for cut in [0, 1, 2, 3, 10, len(good)//2, len(good)-1]:
            with self.subTest(cut=cut): self.assert_rejected(good[:cut])
        self.assert_rejected(b'\x30\x81\x01\0')  # Long form for a short length.
        self.assert_rejected(b'\x30\x85\1\0\0\0\0')

    def test_node_size_and_opaque_ciphertext_bounds(self):
        self.assert_rejected(profile(name_count=50))
        self.assert_rejected(b'\0'*(MAX_CHUNK_BYTES + 1))
        # Ciphertext is opaque, even when bytes resemble recursively nested DER.
        validate_cms_chunk(profile(content=b'\x30\x80'*100 + b'\0\0'))


@unittest.skipUnless(shutil.which('openssl'), 'OpenSSL3+ required')
class OpenSSLProfileTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='artex-cms-profile-')
        cls.root = Path(cls.temporary.name)
        cls.key, cls.cert = cls.root / 'key.pem', cls.root / 'cert.pem'
        cls.run_openssl(['req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '1',
                         '-subj', '/C=US/O=ARTEX synthetic fixture/CN=Backup profile',
                         '-keyout', str(cls.key), '-out', str(cls.cert)])
        cls.key.chmod(0o600)

    @classmethod
    def tearDownClass(cls): cls.temporary.cleanup()

    @staticmethod
    def run_openssl(arguments, data=None):
        result = subprocess.run(['openssl', *arguments], input=data, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=30)
        if result.returncode: raise RuntimeError('Synthetic OpenSSL fixture failed')
        return result.stdout

    def encrypted(self, cipher='-aes-256-gcm', options=None):
        return self.run_openssl(['cms', '-encrypt', '-binary', '-outform', 'DER', cipher,
                                 '-recip', str(self.cert), *(options or [])], b'\0'*9 + b'synthetic payload')

    def test_actual_openssl_profile_and_authenticated_roundtrip(self):
        data = self.encrypted(options=['-keyopt', 'rsa_padding_mode:oaep', '-keyopt', 'rsa_oaep_md:sha256'])
        validate_cms_chunk(data)
        plaintext = self.run_openssl(['cms', '-decrypt', '-binary', '-inform', 'DER', '-inkey', str(self.key)], data)
        self.assertEqual(plaintext, b'\0'*9 + b'synthetic payload')

    def test_actual_cbc_pkcs1_sha1_mgf1_and_aes128_downgrades_refused(self):
        oaep = ['-keyopt', 'rsa_padding_mode:oaep', '-keyopt', 'rsa_oaep_md:sha256']
        cases = [('-aes-256-cbc', oaep), ('-aes-256-gcm', []),
                 ('-aes-256-gcm', ['-keyopt', 'rsa_padding_mode:oaep']),
                 ('-aes-256-gcm', [*oaep, '-keyopt', 'rsa_mgf1_md:sha1']), ('-aes-128-gcm', oaep)]
        for cipher, options in cases:
            with self.subTest(cipher=cipher, options=options):
                with self.assertRaises(ValueError): validate_cms_chunk(self.encrypted(cipher, options))

    def test_restore_rejects_downgrade_before_private_key_decryption(self):
        spec = importlib.util.spec_from_file_location('artex_backup_profile_test', Path(__file__).with_name('artex-backup.py'))
        backup = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(backup)
        data = self.encrypted('-aes-256-cbc', ['-keyopt', 'rsa_padding_mode:oaep', '-keyopt', 'rsa_oaep_md:sha256'])
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            path = Path(temporary) / 'downgrade.cms'
            path.write_bytes(backup.CHUNKED + struct.pack('>I', len(data)) + data)
            with mock.patch.object(backup, 'execute') as decrypt:
                with self.assertRaises(ValueError):
                    backup.decrypt_chunks(path, Path(temporary) / 'plaintext', self.key, 1 << 20)
                decrypt.assert_not_called()


if __name__ == '__main__': unittest.main()

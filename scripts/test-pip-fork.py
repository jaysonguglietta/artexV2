#!/usr/bin/env python3
"""Offline integrity/security regressions for the explicit Python3.14 pip fork."""
import base64
import csv
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('patch_pip', ROOT / 'tools/python/patch-pip.py')
fork = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fork)
PIN = 'f3b60b20ffaadd4415ed7957212a38ac63fa4c5fa375bd0f4a797777940fad13'


def upstream_files():
    removed = 'pkg:pypi/setuptools@70.3.0'
    retained = 'pkg:pypi/urllib3@2.5.0'
    bom = {'bomFormat': 'CycloneDX', 'specVersion': '1.6', 'version': 1,
           'metadata': {'component': {'name': 'pip', 'bom-ref': 'bom-ref:pip', 'type': 'library'}},
           'components': [{'name': 'pip', 'bom-ref': 'bom-ref:pip', 'type': 'library'},
                          {'name': 'setuptools', 'version': '70.3.0', 'bom-ref': removed, 'purl': removed},
                          {'name': 'urllib3', 'version': '2.5.0', 'bom-ref': retained, 'purl': retained}],
           'dependencies': [{'ref': 'bom-ref:pip', 'dependsOn': [removed, retained]},
                            {'ref': removed, 'dependsOn': [retained]}, {'ref': retained, 'dependsOn': []}]}
    return {
        'pip/__init__.py': b'__version__ = "26.3"\n',
        'pip/_internal/metadata/__init__.py': b'if sys.version_info >= (3, 14):\n    # We only support importlib.metadata.\n    return True\n',
        'pip/_internal/metadata/pkg_resources.py': b'legacy backend\n',
        'pip/_vendor/pkg_resources/__init__.py': b'unused vendored component\n',
        'pip/_vendor/pkg_resources/LICENSE': b'removed component license\n',
        'pip/_vendor/bom.cdx.json': json.dumps(bom).encode(),
        'pip/_vendor/vendor.txt': b'setuptools==70.3.0\nurllib3==2.5.0\n',
        'pip/_vendor/urllib3/__init__.py': b'other vendored code must stay unchanged\n',
        'pip-26.3.dist-info/licenses/src/pip/_vendor/pkg_resources/LICENSE': b'removed component license\n',
        'pip-26.3.dist-info/licenses/LICENSE.txt': b'pip license retained\n',
        'pip-26.3.dist-info/METADATA': b'Metadata-Version: 2.4\nName: pip\nVersion: 26.3\nRequires-Python: >=3.9\nLicense-File: LICENSE.txt\nLicense-File: src/pip/_vendor/pkg_resources/LICENSE\n\nUpstream package\n',
        'pip-26.3.dist-info/RECORD': b'',
    }


def wheel(path, files, extra=None):
    with zipfile.ZipFile(path, 'w') as archive:
        for name, data in files.items():
            info = zipfile.ZipInfo(name)
            info.external_attr = (stat.S_IFREG | 0o644) << 16
            archive.writestr(info, data)
        if extra:
            info, data = extra
            archive.writestr(info, data)
    return hashlib.sha256(path.read_bytes()).hexdigest()


class PipForkTests(unittest.TestCase):
    def test_honest_identity_complete_removal_record_and_license_inventory(self):
        self.assertEqual(fork.UPSTREAM_SHA256, PIN)
        self.assertEqual(fork.VERSION, '26.3+artex.1')
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary); original = directory / 'upstream.whl'
            expected = wheel(original, upstream_files())
            first, second = directory / 'first.whl', directory / 'second.whl'
            with patch.object(fork, 'UPSTREAM_SHA256', expected):
                fork.patch_wheel(original, first); fork.patch_wheel(original, second)
            self.assertEqual(first.read_bytes(), second.read_bytes())
            with zipfile.ZipFile(first) as archive:
                names = set(archive.namelist())
                self.assertFalse(any('pkg_resources' in name for name in names))
                self.assertFalse(any(name.startswith('pip-26.3.dist-info/') for name in names))
                self.assertEqual(archive.read('pip/_vendor/urllib3/__init__.py'), upstream_files()['pip/_vendor/urllib3/__init__.py'])
                self.assertNotIn(b'setuptools==', archive.read('pip/_vendor/vendor.txt'))
                before = json.loads(upstream_files()['pip/_vendor/bom.cdx.json'])
                after = json.loads(archive.read('pip/_vendor/bom.cdx.json'))
                removed = 'pkg:pypi/setuptools@70.3.0'
                self.assertEqual(after['version'], before['version'] + 1)
                self.assertEqual(after['metadata']['component']['version'], fork.VERSION)
                self.assertEqual(after['components'], [c for c in before['components'] if c['bom-ref'] != removed])
                expected_dependencies = []
                for dependency in before['dependencies']:
                    if dependency['ref'] == removed: continue
                    dependency = dict(dependency)
                    if 'dependsOn' in dependency:
                        dependency['dependsOn'] = [ref for ref in dependency['dependsOn'] if ref != removed]
                    expected_dependencies.append(dependency)
                self.assertEqual(after['dependencies'], expected_dependencies)
                for dependency in after['dependencies']:
                    self.assertNotEqual(dependency['ref'], removed)
                    self.assertNotIn(removed, dependency.get('dependsOn', []))
                self.assertEqual(after['components'][-1]['version'], '2.5.0', 'unrelated versions must not be relabeled')

                metadata = archive.read(fork.NEW_INFO + 'METADATA').decode()
                self.assertIn('Version: 26.3+artex.1\n', metadata)
                self.assertIn('Requires-Python: >=3.14\n', metadata)
                for line in metadata.splitlines():
                    if line.startswith('License-File: '):
                        self.assertIn(fork.NEW_INFO + 'licenses/' + line.split(': ', 1)[1], names)
                record = list(csv.reader(io.StringIO(archive.read(fork.NEW_INFO + 'RECORD').decode())))
                self.assertEqual({row[0] for row in record}, names)
                for name, digest, size in record:
                    if name.endswith('/RECORD'):
                        self.assertEqual((digest, size), ('', '')); continue
                    data = archive.read(name)
                    encoded = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('=')
                    self.assertEqual(digest, 'sha256=' + encoded)
                    self.assertEqual(int(size), len(data))

    def test_tampering_unsafe_members_and_changed_upstream_proof_fail_closed(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary); source = directory / 'source.whl'; output = directory / 'output.whl'
            source.write_bytes(b'untrusted wheel')
            with self.assertRaises(ValueError): fork.patch_wheel(source, output)
            self.assertFalse(output.exists())
            variations = []
            for name in ('../outside', '/absolute', 'nested\\escape', 'C:drive'):
                info = zipfile.ZipInfo(name); variations.append((upstream_files(), (info, b'bad')))
            info = zipfile.ZipInfo('linked'); info.external_attr = (stat.S_IFLNK | 0o777) << 16
            variations.append((upstream_files(), (info, b'outside')))
            for name, data in [('pip/_internal/metadata/__init__.py', b'no Python 3.14 proof\n'),
                               ('pip/_vendor/vendor.txt', b'setuptools==unexpected\n'),
                               ('pip/__init__.py', b'__version__ = "unreviewed"\n')]:
                files = upstream_files(); files[name] = data; variations.append((files, None))
            files = upstream_files(); bom = json.loads(files['pip/_vendor/bom.cdx.json'])
            bom['components'][1]['version'] = 'unreviewed'
            files['pip/_vendor/bom.cdx.json'] = json.dumps(bom).encode()
            variations.append((files, None))
            for files, extra in variations:
                expected = wheel(source, files, extra)
                with patch.object(fork, 'UPSTREAM_SHA256', expected), self.assertRaises(ValueError):
                    fork.patch_wheel(source, output)
                self.assertFalse(output.exists())

    def test_ensurepip_bundle_replacement_and_unexpected_runtime_are_explicit(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary); ensure = directory / 'ensurepip'; bundled = ensure / '_bundled'
            bundled.mkdir(parents=True)
            init = ensure / '__init__.py'; init.write_text('_PIP_VERSION = "26.2.2"\n')
            previous = bundled / 'pip-26.2.2-py3-none-any.whl'; previous.write_bytes(b'old bundle')
            patched = directory / 'pip-26.3+artex.1-py3-none-any.whl'; patched.write_bytes(b'owned fixture wheel')
            fork.update_ensurepip(ensure, patched)
            self.assertFalse(previous.exists())
            self.assertEqual(list(bundled.iterdir()), [bundled / patched.name])
            self.assertEqual((bundled / patched.name).read_bytes(), patched.read_bytes())
            self.assertIn('_PIP_VERSION = "26.3+artex.1"', init.read_text())
            before = init.read_bytes()
            with self.assertRaises(ValueError): fork.update_ensurepip(ensure, patched)
            self.assertEqual(init.read_bytes(), before)


if __name__ == '__main__':
    unittest.main()

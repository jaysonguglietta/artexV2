#!/usr/bin/env python3
"""Validate the source-only npm security fork; optional installed CLI smoke test."""
import argparse
import copy
import json
from pathlib import Path
import re
import subprocess
import unittest
import urllib.parse

ROOT = Path(__file__).resolve().parent.parent
VERSION = '12.2.3+artex.1'
PINS = {'glob': '14.0.6', 'minimatch': '11.0.9'}


def inspect_fork(manifest, lock):
    if manifest.get('name') != 'npm' or manifest.get('version') != VERSION or manifest.get('private') is not True:
        raise ValueError('npm fork must retain its honest private patched identity')
    if manifest.get('overrides') != PINS or any(manifest.get('dependencies', {}).get(k) != v for k, v in PINS.items()):
        raise ValueError('Every vulnerable npm dependency path must use the exact reviewed pins')
    patch = manifest.get('artexSecurityPatch', {})
    if patch.get('upstream') != 'npm@12.2.3' or patch.get('changes') != PINS:
        raise ValueError('npm fork metadata must describe the actual upstream and dependency changes')
    packages = lock.get('packages')
    if lock.get('lockfileVersion') != 3 or lock.get('version') != VERSION or not isinstance(packages, dict):
        raise ValueError('Missing matching npm fork lock')
    if packages.get('', {}).get('version') != VERSION or packages[''].get('dependencies') != manifest.get('dependencies'):
        raise ValueError('Manifest/lock root identities differ')
    counts = {name: 0 for name in PINS}
    for name, package in packages.items():
        if not name:
            continue
        if not name.startswith('node_modules/') or '..' in name.split('/') or not isinstance(package, dict):
            raise ValueError('Invalid locked dependency path')
        url = urllib.parse.urlparse(package.get('resolved', ''))
        if url.scheme != 'https' or url.netloc != 'registry.npmjs.org' or url.fragment or url.query or package.get('link'):
            raise ValueError('Locked npm dependencies must use exact public registry archives')
        if not re.fullmatch(r'sha512-[A-Za-z0-9+/]+={0,2}', package.get('integrity', '')):
            raise ValueError('Missing locked dependency SHA512 integrity')
        leaf = name.rsplit('node_modules/', 1)[-1]
        if leaf in PINS:
            counts[leaf] += 1
            if package.get('version') != PINS[leaf]:
                raise ValueError('Vulnerable or unreviewed nested glob/minimatch dependency')
    if not all(counts.values()):
        raise ValueError('Patched dependency inventory is incomplete')
    return {'packages': len(packages) - 1, 'patched_dependency_copies': counts, 'version': VERSION}


class NpmForkTests(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads((ROOT / 'tools/npm/package.json').read_text())
        self.lock = json.loads((ROOT / 'tools/npm/package-lock.json').read_text())

    def test_exact_honest_fork_and_entire_locked_graph(self):
        result = inspect_fork(self.manifest, self.lock)
        self.assertGreater(result['packages'], 100)
        self.assertTrue(all(result['patched_dependency_copies'].values()))

    def test_nested_vulnerable_copies_or_missing_integrity_cannot_hide(self):
        for name, package in [('glob', {'version': '13.0.6'}), ('minimatch', {'version': '10.2.6'})]:
            altered = copy.deepcopy(self.lock)
            altered['packages']['node_modules/fixture/node_modules/' + name] = self.lock['packages']['node_modules/' + name] | package
            with self.assertRaises(ValueError):
                inspect_fork(self.manifest, altered)
        altered = copy.deepcopy(self.lock)
        altered['packages']['node_modules/glob'].pop('integrity')
        with self.assertRaises(ValueError):
            inspect_fork(self.manifest, altered)

    def test_unpinned_override_fake_version_or_untrusted_archive_is_refused(self):
        for manifest in [self.manifest | {'version': '12.2.3'}, self.manifest | {'overrides': {}},
                         self.manifest | {'private': False}]:
            with self.assertRaises(ValueError):
                inspect_fork(manifest, self.lock)
        altered = copy.deepcopy(self.lock)
        altered['packages']['node_modules/glob']['resolved'] = 'https://attacker.invalid/glob.tgz'
        with self.assertRaises(ValueError):
            inspect_fork(self.manifest, altered)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--npm-root', type=Path, help='Also exercise an installed patched npm tree using Node')
    args = parser.parse_args()
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(NpmForkTests))
    if not result.wasSuccessful():
        raise SystemExit(1)
    if args.npm_root:
        subprocess.run(['node', str(ROOT / 'tools/npm/test-behavior.mjs'), str(args.npm_root)], check=True, timeout=120)

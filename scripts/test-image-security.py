#!/usr/bin/env python3
"""Offline regression tests for pinned tools and fail-closed image release gates."""
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


scan = module('scan_images', 'scan-images.py')
installer = module('install_trivy', 'install-trivy.py')
NOW = datetime(2026, 11, 18, 20, 0, tzinfo=timezone.utc)
IMAGE = {'image_id': 'sha256:' + '1' * 64, 'platform': 'linux/arm64', 'reference': 'fixture:tag', 'size': 1024, 'config_digest': 'sha256:' + '1' * 64, 'rootfs_diff_ids': ['sha256:' + '3' * 64]}


def policy():
    return json.loads((ROOT / 'security/image-policy.json').read_text())


def report():
    return {'CreatedAt': NOW.isoformat(), 'SchemaVersion': 2, 'Trivy': {'Version': '0.76.2'}, 'ArtifactType': 'container_image',
            'Metadata': {'ImageID': IMAGE['config_digest'], 'DiffIDs': IMAGE['rootfs_diff_ids'], 'ImageConfig': {'os': 'linux', 'architecture': 'arm64'},
                         'OS': {'Family': 'ubuntu', 'Name': '26.04'}},
            'Results': [{'Target': '/private/temp/image.tar (ubuntu 26.04)', 'Class': 'os-pkgs', 'Type': 'ubuntu',
                         'Packages': [{'Name': 'fixture-package', 'Version': '1.0'}]},
                        {'Target': 'usr/local/bin/tool-worker', 'Class': 'lang-pkgs', 'Type': 'gobinary',
                         'Packages': [{'Name': 'github.com/Autumn-27/artex'}, {'Name': 'stdlib', 'Version': 'v1.27.4'}]},
                        {'Target': 'Node.js', 'Class': 'lang-pkgs', 'Type': 'node-pkg',
                         'Packages': [{'Name': 'npm', 'Version': '12.2.3'}, {'Name': 'playwright', 'Version': '1.65.1'},
                                      {'Name': 'playwright-core', 'Version': '1.65.1'}]}]}


def vulnerability(severity='HIGH'):
    return {'VulnerabilityID': 'CVE-2026-12345', 'PkgName': 'fixture-package',
            'InstalledVersion': '1.0', 'Severity': severity, 'Status': 'affected'}


def exception():
    return {'vulnerability_id': 'CVE-2026-12345', 'package': 'fixture-package', 'installed_version': '1.0',
            'component': 'worker', 'target': 'os:ubuntu:26.04', 'platform': 'linux/arm64',
            'reviewed_at': NOW.isoformat(), 'expires': (NOW + timedelta(days=7)).isoformat(),
            'reason': 'Synthetic test for an explicitly reviewed bounded exception.',
            'reference': 'https://github.com/advisories/GHSA-aaaa-bbbb-cccc'}


class GateTests(unittest.TestCase):
    def evaluate(self, value, component='worker', exceptions=None):
        return scan.evaluate_report(value, component, IMAGE, policy(), exceptions or [], '0.76.2')

    def test_all_severities_and_unfixed_findings_remain_visible(self):
        value = report()
        value['Results'][0]['Vulnerabilities'] = [vulnerability(s) for s in sorted(scan.SEVERITIES)]
        result, _ = self.evaluate(value)
        self.assertEqual(sum(result['counts'].values()), 5)
        self.assertEqual({x['severity'] for x in result['blocking']}, {'HIGH', 'CRITICAL', 'UNKNOWN'})
        self.assertTrue(all(x['fixed_version'] is None for x in result['blocking']))

    def test_image_and_platform_identity_must_match(self):
        for field, value in [('ImageID', 'sha256:' + '2' * 64), ('ImageConfig', {'os': 'linux', 'architecture': 'amd64'}), ('ImageConfig', []), ('DiffIDs', ['sha256:' + '4' * 64])]:
            changed = report(); changed['Metadata'][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): self.evaluate(changed)
        for field, value in [('SchemaVersion', 1), ('Trivy', {'Version': 'wrong'}), ('Trivy', [])]:
            changed = report(); changed[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): self.evaluate(changed)

    def test_unknown_or_incomplete_vulnerabilities_fail(self):
        for key, value in [('Severity', 'ALIEN'), ('Severity', None), ('PkgName', ''), ('InstalledVersion', None), ('VulnerabilityID', '')]:
            changed = report(); entry = vulnerability(); entry[key] = value
            changed['Results'][0]['Vulnerabilities'] = [entry]
            with self.subTest(key=key), self.assertRaises(ValueError): self.evaluate(changed)

    def test_missing_inventory_eol_and_wrong_distribution_fail(self):
        for alter in (lambda r: r.pop('Results'), lambda r: r['Results'][0].pop('Packages'),
                      lambda r: r['Results'][0].update(Packages=[]),
                      lambda r: r['Metadata']['OS'].update(EOSL=True),
                      lambda r: r['Metadata']['OS'].update(EOSL='false'),
                      lambda r: r['Metadata']['OS'].update(Name='12'),
                      lambda r: r['Metadata'].pop('OS')):
            changed = report(); alter(changed)
            with self.assertRaises(ValueError): self.evaluate(changed)

    def test_static_application_requires_exact_go_binary_and_module_inventory(self):
        value = report(); value['Metadata'].pop('OS')
        value['Results'] = [{'Target': 'app/artex', 'Class': 'lang-pkgs', 'Type': 'gobinary',
                             'Packages': [{'Name': 'github.com/Autumn-27/artex'}, {'Name': 'stdlib', 'Version': 'v1.27.4'}]}]
        result, _ = self.evaluate(value, 'application')
        self.assertIsNone(result['os'])
        self.assertIn('Not applicable', result['os_scan'])
        for alter in (lambda r: r['Results'][0].update(Target='app/unrelated'),
                      lambda r: r['Results'][0].update(Type='node-pkg'),
                      lambda r: r['Results'][0]['Packages'].pop()):
            changed = copy.deepcopy(value); alter(changed)
            with self.assertRaises(ValueError): self.evaluate(changed, 'application')
        with self.assertRaises(ValueError): self.evaluate(value, 'worker')

    def test_worker_and_broker_require_actual_runtime_inventories(self):
        value = report()
        for target in ('usr/local/bin/tool-worker', 'Node.js'):
            changed = copy.deepcopy(value)
            changed['Results'] = [r for r in changed['Results'] if r['Target'] != target]
            with self.assertRaises(ValueError): self.evaluate(changed)
        changed = copy.deepcopy(value)
        changed['Results'][2]['Packages'][0].pop('Version')
        with self.assertRaises(ValueError): self.evaluate(changed)
        broker = report(); broker['Metadata']['OS'] = {'Family': 'alpine', 'Name': '3.24.4'}
        broker['Results'][0]['Type'] = 'alpine'
        broker['Results'][1]['Target'] = 'usr/local/bin/worker-broker'
        broker['Results'][2] = {'Target': 'usr/local/bin/docker', 'Class': 'lang-pkgs', 'Type': 'gobinary',
                                'Packages': [{'Name': 'stdlib', 'Version': 'v1.27.4'}]}
        result, _ = self.evaluate(broker, 'broker')
        self.assertEqual(len(result['required_go_inventory']), 2)
        for target in ('usr/local/bin/worker-broker', 'usr/local/bin/docker'):
            changed = copy.deepcopy(broker)
            changed['Results'] = [r for r in changed['Results'] if r['Target'] != target]
            with self.assertRaises(ValueError): self.evaluate(changed, 'broker')

    def test_exception_exactness_and_stale_selection(self):
        value = report(); value['Results'][0]['Vulnerabilities'] = [vulnerability()]
        item = exception(); validated = policy(); validated['exceptions'] = [item]
        entries = scan.validate_policy(validated, NOW)
        result, matched = self.evaluate(value, exceptions=entries)
        self.assertFalse(result['blocking']); self.assertEqual(len(result['reviewed_exceptions']), 1)
        for key in ('package', 'installed_version', 'target', 'platform'):
            changed = copy.deepcopy(item); changed[key] += '-different'
            result, _ = self.evaluate(value, exceptions=[changed])
            self.assertEqual(len(result['blocking']), 1)
        scan.check_unused_exceptions(entries, set(), {('broker', 'linux/arm64')})
        scan.check_unused_exceptions(entries, set(), {('worker', 'linux/amd64')})
        with self.assertRaises(ValueError): scan.check_unused_exceptions(entries, set(), {('worker', 'linux/arm64')})
        scan.check_unused_exceptions(entries, matched, {('worker', 'linux/arm64')})

    def test_expired_wildcard_oversized_unknown_exceptions_fail(self):
        mutations = [('expires', (NOW - timedelta(seconds=1)).isoformat()),
                     ('expires', (NOW + timedelta(days=31)).isoformat()),
                     ('reviewed_at', (NOW + timedelta(days=1)).isoformat()),
                     ('package', '*'), ('component', 'unknown'), ('platform', 'linux/unknown'),
                     ('vulnerability_id', 'all'), ('reference', 'http://example.invalid'), ('reason', 'short')]
        for key, value in mutations:
            changed = policy(); item = exception(); item[key] = value; changed['exceptions'] = [item]
            with self.subTest(key=key), self.assertRaises(ValueError): scan.validate_policy(changed, NOW)
        changed = policy(); changed['exceptions'] = [exception(), exception()]
        with self.assertRaises(ValueError): scan.validate_policy(changed, NOW)

    def test_scanner_suppression_is_not_a_waiver(self):
        value = report(); value['Results'][0]['ExperimentalModifiedFindings'] = [
            {'Type': 'vulnerability', 'Finding': vulnerability()}]
        result, _ = self.evaluate(value)
        self.assertEqual(len(result['blocking']), 1)
        value['Results'][0]['ExperimentalModifiedFindings'] = [{'unexpected': 'schema'}]
        with self.assertRaises(ValueError): self.evaluate(value)

    def test_database_missing_stale_future_and_wrong_schema_fail(self):
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary); (cache / 'db').mkdir(); (cache / 'db/trivy.db').write_bytes(b'fixture database')
            metadata = {'Version': 2, 'UpdatedAt': NOW.isoformat(), 'DownloadedAt': NOW.isoformat(), 'NextUpdate': (NOW + timedelta(hours=6)).isoformat()}
            path = cache / 'db/metadata.json'; path.write_text(json.dumps(metadata))
            identity = scan.database_identity(cache, policy(), NOW)
            self.assertEqual(len(identity['database_sha256']), 64)
            for key, value in [('UpdatedAt', (NOW - timedelta(hours=73)).isoformat()),
                               ('UpdatedAt', (NOW + timedelta(hours=1)).isoformat()),
                               ('DownloadedAt', None), ('Version', 1)]:
                changed = dict(metadata); changed[key] = value; path.write_text(json.dumps(changed))
                with self.subTest(key=key), self.assertRaises(ValueError): scan.database_identity(cache, policy(), NOW)

    def test_ambient_config_cannot_filter_results(self):
        clean = scan.sanitized_environment({'PATH': '/usr/bin', 'TRIVY_IGNORE_UNFIXED': 'true',
                    'TRIVY_SEVERITY': 'LOW', 'TRIVY_CONFIG': '/host/policy', 'TrIvY_SKIP_FILES': '*',
                    'DOCKER_HOST': 'unix:///socket', 'DOCKER_CONFIG': '/host/credentials'}, '/empty')
        self.assertFalse(any(k.upper().startswith('TRIVY_') for k in clean))
        self.assertNotIn('DOCKER_HOST', clean); self.assertEqual(clean['DOCKER_CONFIG'], '/empty')
        args = scan.scan_arguments('/trivy', '/cache', '/exact.tar', '/result.json')
        self.assertEqual(args[args.index('--config') + 1], '')
        self.assertEqual(args[args.index('--ignorefile') + 1], '')
        self.assertIn('--ignore-unfixed=false', args); self.assertIn('--show-suppressed', args)
        self.assertNotIn('--skip-files', args); self.assertNotIn('--skip-dirs', args)

    def test_docker_inspection_validates_explicit_platform(self):
        output = json.dumps([{'Id': IMAGE['image_id'], 'Os': 'linux', 'Architecture': 'arm64', 'Size': 1024, 'RootFS': {'Type': 'layers', 'Layers': IMAGE['rootfs_diff_ids']}}])
        with patch.object(scan, 'run', return_value=output) as command:
            result = scan.inspect_image('fixture:tag', 'linux/arm64', policy())
            self.assertEqual(result['image_id'], IMAGE['image_id'])
            self.assertIn('--', command.call_args.args[0])
            with self.assertRaises(ValueError): scan.inspect_image('fixture:tag', 'linux/amd64', policy())


class WithdrawalTests(unittest.TestCase):
    def npm_report(self):
        value = report()
        value['Results'].append({'Target': 'Node.js', 'Class': 'lang-pkgs', 'Type': 'node-pkg',
            'Packages': [{'Name': 'fixture-package', 'Version': '1.0'}], 'Vulnerabilities': [vulnerability()]})
        return value

    def evidence(self):
        return {'source': 'https://api.github.com/advisories?cve_id=CVE-2026-12345&ecosystem=npm&per_page=100',
                'response_sha256': 'a' * 64, 'advisories': [{'ghsa_id': 'GHSA-aaaa-bbbb-cccc',
                'cve_id': 'CVE-2026-12345', 'withdrawn_at': (NOW - timedelta(hours=1)).isoformat(),
                'vulnerabilities': [{'package': {'ecosystem': 'npm', 'name': 'fixture-package'}}]}]}

    def evaluate(self, verifier, value=None):
        return scan.evaluate_report(value or self.npm_report(), 'worker', IMAGE, policy(), [], '0.76.2', verifier)[0]

    def test_withdrawal_keeps_raw_counts_and_exact_primary_proof_cached_per_run(self):
        fetch = unittest.mock.Mock(return_value=self.evidence())
        verifier = scan.WithdrawalVerifier(fetch, NOW)
        for _ in range(2):
            result = self.evaluate(verifier)
            self.assertEqual(result['counts']['HIGH'], 1)
            self.assertFalse(result['blocking'])
            self.assertFalse(result['reviewed_exceptions'])
            self.assertEqual(len(result['verified_withdrawn']), 1)
            proof = result['verified_withdrawn'][0]['proof']
            self.assertEqual(proof['classification'], 'verified_withdrawn')
            self.assertEqual(proof['advisories'], self.evidence()['advisories'])
        fetch.assert_called_once_with('CVE-2026-12345')
        os_only = report(); os_only['Results'][0]['Vulnerabilities'] = [vulnerability()]
        result = self.evaluate(verifier, os_only)
        self.assertEqual(len(result['blocking']), 1)
        self.assertFalse(result['verified_withdrawn'])

    def test_active_ambiguous_wrong_package_and_identity_evidence_fail_closed(self):
        changes = [lambda x: x.update(withdrawn_at=None),
                   lambda x: x.update(withdrawn_at=(NOW + timedelta(seconds=1)).isoformat()),
                   lambda x: x.update(cve_id='CVE-2026-54321'),
                   lambda x: x['vulnerabilities'][0]['package'].update(name='other-package'),
                   lambda x: x['vulnerabilities'][0]['package'].update(ecosystem='Go')]
        for change in changes:
            evidence = self.evidence(); change(evidence['advisories'][0])
            result = self.evaluate(scan.WithdrawalVerifier(lambda _: evidence, NOW))
            self.assertEqual(len(result['blocking']), 1)
            self.assertFalse(result['verified_withdrawn'])
        for records in ([], [{}, {}]):
            evidence = self.evidence(); evidence['advisories'] = records
            self.assertEqual(len(self.evaluate(scan.WithdrawalVerifier(lambda _: evidence, NOW))['blocking']), 1)
        evidence = self.evidence(); active = copy.deepcopy(evidence['advisories'][0]); active.update(ghsa_id='GHSA-dddd-eeee-ffff', withdrawn_at=None)
        evidence['advisories'].append(active)
        self.assertEqual(len(self.evaluate(scan.WithdrawalVerifier(lambda _: evidence, NOW))['blocking']), 1)

    def test_unavailable_api_redirects_and_untrusted_urls_never_authorize(self):
        fetch = unittest.mock.Mock(side_effect=OSError('public API unavailable'))
        verifier = scan.WithdrawalVerifier(fetch, NOW)
        for _ in range(2):
            result = self.evaluate(verifier)
            self.assertIn('unavailable', result['blocking'][0]['withdrawal_verification_error'])
        fetch.assert_called_once()
        with self.assertRaises(ValueError):
            scan.RefuseAdvisoryRedirects().redirect_request(None, None, 302, '', {}, 'https://elsewhere.invalid')
        with patch.object(scan.urllib.request, 'build_opener') as opener:
            for identifier in ('https://attacker.invalid/CVE-2026-12345', 'CVE-2026-12345?token=unexpected', 'all'):
                with self.assertRaises(ValueError): scan.fetch_official_advisories(identifier)
            opener.assert_not_called()
        # A report-provided URL is never used for a withdrawal lookup.
        value = self.npm_report(); value['Results'][-1]['Vulnerabilities'][0]['PrimaryURL'] = 'http://169.254.169.254/private'
        safe = unittest.mock.Mock(return_value=self.evidence())
        self.assertFalse(self.evaluate(scan.WithdrawalVerifier(safe, NOW), value)['blocking'])
        safe.assert_called_once_with('CVE-2026-12345')


class ArchiveIdentityTests(unittest.TestCase):
    def fixture(self, oci):
        def encoded(value): return json.dumps(value, sort_keys=True).encode()
        def digest(value): return 'sha256:' + hashlib.sha256(value).hexdigest()
        def path(value): return 'blobs/sha256/' + digest(value).split(':')[1]
        layer = b'bounded synthetic layer bytes'
        diff_ids = [digest(layer)]
        config = encoded({'os': 'linux', 'architecture': 'arm64', 'rootfs': {'type': 'layers', 'diff_ids': diff_ids}})
        config_path = path(config) if oci else digest(config).split(':')[1] + '.json'
        layer_path = path(layer) if oci else 'layer/layer.tar'
        files = {config_path: config, layer_path: layer}
        image_id = digest(config)
        if oci:
            manifest = encoded({'schemaVersion': 2, 'config': {'digest': digest(config), 'size': len(config)},
                'layers': [{'digest': digest(layer), 'size': len(layer), 'mediaType': 'application/vnd.oci.image.layer.v1.tar'}]})
            index = encoded({'schemaVersion': 2, 'manifests': [{'digest': digest(manifest), 'size': len(manifest),
                'platform': {'os': 'linux', 'architecture': 'arm64'}}]})
            files[path(manifest)] = manifest; files[path(index)] = index
            files['index.json'] = encoded({'schemaVersion': 2, 'manifests': [{'digest': digest(index), 'size': len(index)}]})
            image_id = digest(index)
        files['manifest.json'] = encoded([{'Config': config_path, 'Layers': [layer_path]}])
        return files, {'image_id': image_id, 'platform': 'linux/arm64', 'rootfs_diff_ids': diff_ids}, config_path, layer_path

    def evaluate(self, files, image):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / 'image.tar'
            with tarfile.open(archive, 'w') as bundle:
                for name, data in files.items():
                    entry = tarfile.TarInfo(name); entry.size = len(data)
                    bundle.addfile(entry, io.BytesIO(data))
            return scan.archive_identity(archive, image, 1024 * 1024)

    def test_classic_and_oci_index_ids_bind_exact_configs_and_layers(self):
        for oci in (False, True):
            files, image, config, _ = self.fixture(oci)
            result = self.evaluate(files, image)
            self.assertEqual(result['config_digest'], 'sha256:' + hashlib.sha256(files[config]).hexdigest())
            self.assertEqual(result['rootfs_diff_ids'], image['rootfs_diff_ids'])
            self.assertEqual(result['archive_identity']['format'], 'oci-chain' if oci else 'docker-config')
            self.assertEqual(result['archive_identity']['exported_image_id'], image['image_id'])
            self.assertEqual(len(result['archive_identity']['oci_chain']), 2 if oci else 0)

    def test_tampered_config_layer_digest_platform_or_root_index_fails(self):
        for oci in (False, True):
            for field in ('config', 'layer', 'platform', 'root', 'diffids'):
                files, image, config_path, layer_path = self.fixture(oci)
                if field == 'config': files[config_path] += b' '
                elif field == 'layer': files[layer_path] = b'modified synthetic contents'
                elif field == 'platform': image['platform'] = 'linux/amd64'
                elif field == 'root': image['image_id'] = 'sha256:' + '9' * 64
                else: image['rootfs_diff_ids'] = ['sha256:' + '8' * 64]
                with self.subTest(oci=oci, field=field), self.assertRaises(ValueError): self.evaluate(files, image)


class InstallerTests(unittest.TestCase):
    def archive(self, directory, *, symbolic=False, name='trivy'):
        path = Path(directory) / 'release.tar.gz'
        with tarfile.open(path, 'w:gz') as bundle:
            entry = tarfile.TarInfo(name); entry.size = 4
            if symbolic:
                entry.type = tarfile.SYMTYPE; entry.linkname = '/outside'
                bundle.addfile(entry)
            else: bundle.addfile(entry, io.BytesIO(b'tool'))
        return path

    def test_checksum_tampering_and_nonregular_binary_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = self.archive(temporary)
            self.assertEqual(installer.binary_from_archive(path, installer.sha256_file(path)), b'tool')
            with self.assertRaises(ValueError): installer.binary_from_archive(path, '0' * 64)
        with tempfile.TemporaryDirectory() as temporary:
            path = self.archive(temporary, symbolic=True)
            with self.assertRaises(ValueError): installer.binary_from_archive(path, installer.sha256_file(path))
        with tempfile.TemporaryDirectory() as temporary:
            path = self.archive(temporary, name='../trivy')
            with self.assertRaises(ValueError): installer.binary_from_archive(path, installer.sha256_file(path))

    def test_existing_binary_tampering_is_not_trusted(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / 'trivy'; binary.write_bytes(b'tampered'); binary.chmod(0o700)
            pins = {'version': '0.76.2'}; asset = {'binary_sha256': hashlib.sha256(b'official').hexdigest()}
            with patch.object(installer, 'release_pin', return_value=(pins, asset)):
                with self.assertRaises(ValueError): installer.install(binary)
            link = Path(temporary) / 'linked'; link.symlink_to(binary)
            with self.assertRaises(ValueError): installer.existing_digest(link)


if __name__ == '__main__':
    unittest.main()

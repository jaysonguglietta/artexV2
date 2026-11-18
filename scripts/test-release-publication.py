#!/usr/bin/env python3
"""No-network publication orchestration tests: scanning gates and refusal paths."""
import importlib.util
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import urllib.error

ROOT = Path(__file__).resolve().parent


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / filename)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


images = module('release_images', 'release-images.py')
publish = module('publish_signed_release', 'publish-signed-release.py')


class ImagePublicationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='artex-release-publish-tests-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.output = self.root / 'dist/container-metadata'
        self.context = images.release.context('release', 'refs/tags/v1.2.3', 'a' * 40, '123', '1')
        self.calls = []
        self.scan_failure = False
        self.verified = []
        self.binding_failure = False
        self.index_failure = False

    def scan_inventory(self, arch):
        return {'status': 'passed', 'images': [{
            'component': name, 'platform': 'linux/' + arch, 'blocking': [],
            'config_digest': 'sha256:' + hashlib.sha256((name + arch).encode()).hexdigest(),
            'image_id': 'sha256:' + 'f' * 64,  # Docker 29 may expose an OCI index ID.
            'rootfs_diff_ids': ['sha256:' + 'a' * 64],
        } for name in images.COMPONENTS]}

    def command(self, arguments, **kwargs):
        self.calls.append(arguments)
        if len(arguments) > 1 and arguments[1] == 'scripts/scan-images.py':
            if self.scan_failure and 'linux/arm64' in arguments:
                raise subprocess.CalledProcessError(1, arguments)
            directory = Path(arguments[arguments.index('--output') + 1])
            directory.mkdir(parents=True)
            arch = arguments[arguments.index('--platform') + 1].removeprefix('linux/')
            (directory / 'summary.json').write_text(json.dumps(self.scan_inventory(arch)))
        return subprocess.CompletedProcess(arguments, 0, '', '')

    def verify_image(self, repository, digest, identity):
        self.verified.append(identity)
        if self.binding_failure:
            raise ValueError('Pushed manifest config differs from the scanned config')
        return identity | {'digest': digest, 'push_digest': digest}

    def invoke(self):
        with mock.patch.object(images, 'ROOT', self.root), \
             mock.patch.object(images.release, 'gate', return_value=self.context), \
             mock.patch.object(images, 'command', side_effect=self.command), \
             mock.patch.object(images, 'verify_pushed_image', side_effect=self.verify_image), \
             mock.patch.object(images, 'registry_document', return_value={}), \
             mock.patch.object(images, 'index_children', side_effect=ValueError('index substitution') if self.index_failure else None), \
             mock.patch.object(images, 'manifest_digest', side_effect=lambda repo, tag, absent=False: None if absent else 'sha256:' + 'b' * 64):
            images.build_scan_publish(self.output)

    def test_every_architecture_scan_precedes_first_push(self):
        self.invoke()
        scans = [i for i, command in enumerate(self.calls) if 'scripts/scan-images.py' in command]
        pushes = [i for i, command in enumerate(self.calls) if command[:2] == ['docker', 'push']]
        self.assertEqual(len(scans), 2)
        self.assertEqual(len(pushes), 8)
        self.assertLess(max(scans), min(pushes))
        for index in scans:
            command = self.calls[index]
            for component in ('application', 'worker', 'broker', 'postgres'):
                self.assertTrue(any(value.startswith(component + '=') for value in command))
        metadata = json.loads((self.output / 'images.json').read_text())
        self.assertEqual(set(metadata), {'application', 'worker', 'broker', 'postgres'})
        builds = [command for command in self.calls if command[:3] == ['docker', 'buildx', 'build']]
        self.assertEqual(len(builds), 8)
        self.assertEqual(sum(command[command.index('--target') + 1] == 'postgres' for command in builds), 2)
        self.assertFalse(any(command[:2] == ['docker', 'pull'] for command in self.calls))
        self.assertTrue(all('@sha256:' not in item['name'] and item['digest'].startswith('sha256:')
                            for item in metadata.values()))
        self.assertFalse(any(':latest' in value for command in self.calls for value in command))
        self.assertEqual(len(self.verified), 8)
        self.assertTrue(all(identity['config_digest'] != 'sha256:' + 'f' * 64 for identity in self.verified))

    def test_arm_scan_failure_prevents_all_publication(self):
        self.scan_failure = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke()
        self.assertFalse(any(command[:2] == ['docker', 'push'] for command in self.calls))
        self.assertFalse((self.output / 'images.json').exists())

    def test_substituted_child_prevents_index_and_signing_metadata(self):
        self.binding_failure = True
        with self.assertRaisesRegex(ValueError, 'scanned config'):
            self.invoke()
        self.assertFalse(any(command[:3] == ['docker', 'buildx', 'imagetools'] for command in self.calls))
        self.assertFalse((self.output / 'images.json').exists())
        self.assertFalse((self.output / 'image-scan-summary.json').exists())

    def test_substituted_index_prevents_signing_metadata(self):
        self.index_failure = True
        with self.assertRaisesRegex(ValueError, 'index substitution'):
            self.invoke()
        self.assertFalse((self.output / 'images.json').exists())
        self.assertFalse((self.output / 'image-scan-summary.json').exists())

    def test_registry_only_404_means_absent(self):
        for code in [401, 403, 429, 500, 503]:
            error = urllib.error.HTTPError('https://ghcr.io/fixture', code, 'fixture', {}, None)
            with mock.patch.object(images, 'registry_token', return_value='fixture'), \
                 mock.patch.object(images.OPENER, 'open', side_effect=error):
                with self.subTest(code=code), self.assertRaises(ValueError):
                    images.manifest_digest('fixture', 'v1.2.3', absent=True)
        error = urllib.error.HTTPError('https://ghcr.io/fixture', 404, 'fixture', {}, None)
        with mock.patch.object(images, 'registry_token', return_value='fixture'), \
             mock.patch.object(images.OPENER, 'open', side_effect=error):
            self.assertIsNone(images.manifest_digest('fixture', 'v1.2.3', absent=True))

    def test_existing_version_tag_refused(self):
        response = mock.MagicMock()
        response.__enter__.return_value.headers.get.return_value = 'sha256:' + 'b' * 64
        with mock.patch.object(images, 'registry_token', return_value='fixture'), \
             mock.patch.object(images.OPENER, 'open', return_value=response):
            with self.assertRaisesRegex(ValueError, 'replace'):
                images.manifest_digest('fixture', 'v1.2.3', absent=True)

    def test_manifest_checks_refresh_expiring_tokens_after_uploads(self):
        response = mock.MagicMock()
        response.__enter__.return_value.headers.get.return_value = 'sha256:' + 'b' * 64
        with mock.patch.object(images, 'registry_token', side_effect=['fresh-before-upload', 'fresh-after-upload']) as token, \
             mock.patch.object(images.OPENER, 'open', return_value=response) as request:
            for tag in ['v1.2.3-amd64', 'v1.2.3-arm64']:
                self.assertEqual(images.manifest_digest('fixture', tag), 'sha256:' + 'b' * 64)
        self.assertEqual(token.call_args_list, [mock.call('fixture'), mock.call('fixture')])
        self.assertEqual([call.args[0].get_header('Authorization') for call in request.call_args_list],
                         ['Bearer fresh-before-upload', 'Bearer fresh-after-upload'])


class RegistryIdentityTests(unittest.TestCase):
    def setUp(self):
        self.repository = images.release.IMAGE_ROOT.removeprefix('ghcr.io/') + '/application'
        self.config = 'sha256:' + 'a' * 64
        self.digest = 'sha256:' + 'b' * 64
        self.layer = {'mediaType': 'application/vnd.oci.image.layer.v1.tar+gzip',
                      'digest': 'sha256:' + 'c' * 64, 'size': 32}
        self.manifest = {'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.manifest.v1+json',
                         'config': {'mediaType': 'application/vnd.oci.image.config.v1+json',
                                    'digest': self.config, 'size': 256}, 'layers': [self.layer]}
        self.identity = {'config_digest': self.config, 'rootfs_diff_ids': ['sha256:' + 'd' * 64],
                         'platform': 'linux/amd64'}

    def index(self, children):
        return {'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.index.v1+json',
                'manifests': [{'mediaType': self.manifest['mediaType'], 'digest': digest, 'size': 512,
                               'platform': {'os': 'linux', 'architecture': arch}}
                              for arch, digest in children.items()]}

    def test_registry_bytes_are_bounded_hash_authenticated_and_host_fixed(self):
        raw = json.dumps(self.manifest).encode()
        digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
        response = mock.MagicMock()
        response.__enter__.return_value.read.return_value = raw
        response.__enter__.return_value.headers.get.return_value = self.manifest['mediaType']
        with mock.patch.object(images, 'registry_token', return_value='fixture'), \
             mock.patch.object(images.OPENER, 'open', return_value=response) as request:
            self.assertEqual(images.registry_document(self.repository, digest), self.manifest)
            self.assertEqual(request.call_args.args[0].full_url,
                             'https://ghcr.io/v2/' + self.repository + '/manifests/' + digest)
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                images.registry_document(self.repository, self.digest)
            with self.assertRaisesRegex(ValueError, 'Untrusted registry'):
                images.registry_document('attacker/untrusted', digest)
            response.__enter__.return_value.read.return_value = b'x' * (images.MAX_REGISTRY_JSON + 1)
            with self.assertRaisesRegex(ValueError, 'bounded JSON'):
                images.registry_document(self.repository, digest)
        with self.assertRaisesRegex(ValueError, 'redirects'):
            images.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://attacker.invalid/')

    def test_manifest_config_binding_and_docker29_single_platform_index(self):
        with mock.patch.object(images, 'registry_document', return_value=self.manifest):
            result = images.verify_pushed_image(self.repository, self.digest, self.identity)
            self.assertEqual(result['config_digest'], self.config)
            self.assertEqual(result['rootfs_diff_ids'], self.identity['rootfs_diff_ids'])
        index = self.index({'amd64': self.digest})
        root_digest = 'sha256:' + 'e' * 64
        with mock.patch.object(images, 'registry_document', side_effect=[index, self.manifest]):
            result = images.verify_pushed_image(self.repository, root_digest, self.identity)
            self.assertEqual(result['digest'], self.digest)
            self.assertEqual(result['push_digest'], root_digest)

    def test_substituted_config_foreign_urls_inline_data_and_invalid_descriptors_refused(self):
        for changed in [
            self.manifest | {'config': self.manifest['config'] | {'digest': 'sha256:' + 'f' * 64}},
            self.manifest | {'config': self.manifest['config'] | {'urls': ['https://attacker.invalid/config']}},
            self.manifest | {'layers': [self.layer | {'data': 'inline'}]},
            self.manifest | {'layers': [self.layer | {'urls': []}]},
            self.manifest | {'layers': [self.layer | {'size': -1}]},
            self.manifest | {'layers': [self.layer | {'size': True}]},
            self.manifest | {'layers': [self.layer | {'digest': 'latest'}]},
            self.manifest | {'layers': [self.layer | {'mediaType': 'application/vnd.docker.image.rootfs.foreign.diff.tar.gzip'}]},
            self.manifest | {'layers': []},
        ]:
            with self.subTest(manifest=changed), mock.patch.object(images, 'registry_document', return_value=changed):
                with self.assertRaises(ValueError):
                    images.verify_pushed_image(self.repository, self.digest, self.identity)

    def test_index_child_substitution_platform_and_closed_inventory_refused(self):
        expected = {'amd64': self.digest, 'arm64': 'sha256:' + 'e' * 64}
        correct = self.index(expected)
        self.assertEqual(set(images.index_children(correct, expected)), set(expected))
        for children in [{'amd64': self.digest}, expected | {'arm64': 'sha256:' + 'f' * 64}]:
            with self.assertRaises(ValueError):
                images.index_children(self.index(children), expected)
        for change in [{'os': 'windows'}, {'architecture': 'arm'}, {'variant': 'untrusted'}]:
            altered = json.loads(json.dumps(correct))
            altered['manifests'][0]['platform'].update(change)
            with self.assertRaises(ValueError):
                images.index_children(altered, expected)
        altered = json.loads(json.dumps(correct))
        altered['manifests'][0]['urls'] = ['https://attacker.invalid/image']
        with self.assertRaises(ValueError):
            images.index_children(altered, expected)

    def test_missing_scan_identity_or_architecture_inventory_is_refused(self):
        fixture = ImagePublicationTests()
        good = fixture.scan_inventory('amd64')
        self.assertEqual(set(images.scanned_identities(good, 'amd64')), set(images.COMPONENTS))
        for bad in [{'status': 'passed', 'images': []}, good | {'status': 'blocked'},
                    good | {'images': good['images'][:-1]},
                    good | {'images': [record | {'config_digest': None} for record in good['images']]},
                    good | {'images': [record | {'rootfs_diff_ids': []} for record in good['images']]}]:
            with self.assertRaises(ValueError):
                images.scanned_identities(bad, 'amd64')
        with self.assertRaises(ValueError):
            images.scanned_identities(good, 'arm64')

    def test_push_digest_preferred_and_fallback_stays_verification_bound(self):
        result = subprocess.CompletedProcess([], 0, 'v1.2.3-amd64: digest: ' + self.digest + ' size: 512\n', '')
        with mock.patch.object(images, 'manifest_digest') as head:
            self.assertEqual(images.pushed_digest(result, self.repository, 'v1.2.3-amd64'), self.digest)
            head.assert_not_called()
        result.stdout = 'different Docker output\n'
        with mock.patch.object(images, 'manifest_digest', return_value=self.digest) as head:
            self.assertEqual(images.pushed_digest(result, self.repository, 'v1.2.3-amd64'), self.digest)
            head.assert_called_once_with(self.repository, 'v1.2.3-amd64')


class ReleasePublicationTests(unittest.TestCase):
    def test_candidate_cannot_call_release_creation(self):
        with mock.patch.object(publish.r, 'gate', return_value={'mode': 'candidate'}), \
             mock.patch.object(publish.r, 'run') as run:
            with self.assertRaises(ValueError):
                publish.publish(Path('unused'))
            run.assert_not_called()

    def test_existing_release_refused_before_creation(self):
        response = mock.MagicMock()
        opener = mock.MagicMock()
        opener.open.return_value = response
        with mock.patch.object(publish.r, 'gate', return_value={'mode': 'release', 'ref': 'refs/tags/v1.2.3'}), \
             mock.patch.object(publish.r, 'verify'), \
             mock.patch.object(publish.r, 'run') as run, \
             mock.patch.dict(publish.os.environ, {'GH_TOKEN': 'synthetic-test-token'}), \
             mock.patch.object(publish.urllib.request, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(ValueError, 'existing'):
                publish.publish(Path('unused'))
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()

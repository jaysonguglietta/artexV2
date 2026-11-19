#!/usr/bin/env python3
"""Local cryptographic/metadata/path/gate tests; live Fulcio tests are separate.

OpenSSL keys here are ephemeral fixtures, never release signing keys. These tests
DO NOT replace test-keyless-release.py with real GitHub OIDC/Fulcio/Rekor proofs.
"""
import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import unittest
from unittest import mock
import zipfile

spec = importlib.util.spec_from_file_location('signed_release', Path(__file__).with_name('signed-release.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)
SHA = 'a' * 40


class MetadataTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='artex-signature-tests-')
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name)
        self.ctx = r.context('candidate', 'refs/heads/main', SHA, '123', '1')
        self.archive = self.directory / r.archive_names(self.ctx)[0]
        with zipfile.ZipFile(self.archive, 'w') as archive:
            archive.writestr('candidate.txt', 'fixture')
        (self.directory / 'images.json').write_text(json.dumps({'candidate': {
            'name': r.CANDIDATE_IMAGE, 'digest': 'sha256:' + 'b' * 64}}))
        r.prepare(self.directory, self.ctx)

    def test_valid_metadata_and_explicit_candidate_boundary(self):
        self.assertIn('candidate', r.verify_metadata(self.directory, self.ctx))
        other = r.context('release', 'refs/tags/v1.2.3', SHA, '123', '1')
        with self.assertRaises((ValueError, OSError)):
            r.verify_metadata(self.directory, other)

    def test_tampered_archive_rejected(self):
        self.archive.write_bytes(self.archive.read_bytes() + b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum'):
            r.verify_metadata(self.directory, self.ctx)

    def test_signed_provenance_must_match_fixed_repository_workflow_ref_sha_and_run(self):
        path = self.directory / 'provenance.json'
        original = json.loads(path.read_text())
        for field, value in [('repository', 'attacker/untrusted'), ('workflow', '.github/workflows/other.yml'),
                             ('ref', 'refs/tags/v1.2.3'), ('commit', '0' * 40), ('run_id', '999')]:
            with self.subTest(field=field):
                changed = copy.deepcopy(original)
                changed['predicate']['buildDefinition']['externalParameters'][field] = value
                path.write_text(json.dumps(changed))
                with self.assertRaisesRegex(ValueError, 'Provenance'):
                    r.verify_metadata(self.directory, self.ctx)
        path.write_text(json.dumps(original))

    def test_checksum_paths_and_duplicates_rejected_without_outside_reads(self):
        path = self.directory / 'SHA256SUMS'
        original = path.read_text()
        for payload in ['../outside', '/absolute', 'a/b', 'a\\b', '--argument', 'images.json\nother']:
            path.write_text('a' * 64 + '  ' + payload + '\n')
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                r.verify_metadata(self.directory, self.ctx)
        path.write_text(original + original)
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            r.verify_metadata(self.directory, self.ctx)

    def test_symlink_and_unexpected_zip_rejected(self):
        link = self.directory / 'link.json'
        link.symlink_to(self.directory / 'images.json')
        with self.assertRaisesRegex(ValueError, 'regular'):
            r.regular(self.directory, link.name)
        (self.directory / 'extra.zip').write_bytes(self.archive.read_bytes())
        with self.assertRaisesRegex(ValueError, 'Archive set'):
            r.verify_metadata(self.directory, self.ctx)

    def test_unsafe_zip_members_rejected(self):
        for name in ['../outside', '/absolute', 'a\\b', 'C:drive', 'a/./b', 'a//b']:
            path = self.directory / 'unsafe.zip'
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr(name, 'fixture')
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'Unsafe'):
                r.check_zip(path)
        path = self.directory / 'symlink.zip'
        with zipfile.ZipFile(path, 'w') as archive:
            item = zipfile.ZipInfo('link')
            item.create_system = 3
            item.external_attr = (stat.S_IFLNK | 0o777) << 16
            archive.writestr(item, '../outside')
        with self.assertRaisesRegex(ValueError, 'links'):
            r.check_zip(path)

    def test_image_reference_cannot_choose_other_registry_repository_or_tag(self):
        path = self.directory / 'images.json'
        original = json.loads(path.read_text())
        for field, value in [('name', 'ghcr.io/attacker/image'), ('name', r.IMAGE_ROOT + '/application'),
                             ('digest', 'latest'), ('digest', 'sha256:' + 'f' * 63)]:
            changed = copy.deepcopy(original)
            changed['candidate'][field] = value
            path.write_text(json.dumps(changed))
            with self.subTest(value=value), self.assertRaises(ValueError):
                r.images(self.directory, self.ctx)

    def test_release_requires_all_four_exact_component_repositories(self):
        ctx = r.context('release', 'refs/tags/v1.2.3', SHA, '123', '1')
        records = {name: {'name': r.IMAGE_ROOT + '/' + name, 'digest': 'sha256:' + 'b' * 64}
                   for name in ('application', 'worker', 'broker', 'postgres')}
        path = self.directory / 'images.json'
        path.write_text(json.dumps(records))
        self.assertEqual(set(r.images(self.directory, ctx)), set(records))
        for changed in [dict(list(records.items())[:-1]),
                        records | {'postgres': {'name': 'docker.io/library/postgres',
                                                'digest': 'sha256:' + 'b' * 64}}]:
            path.write_text(json.dumps(changed))
            with self.assertRaises(ValueError):
                r.images(self.directory, ctx)

    def test_certificate_identity_flags_are_exact_and_not_artifact_driven(self):
        flags = r.certificate_flags(self.ctx)
        self.assertEqual(flags[flags.index('--certificate-oidc-issuer') + 1], r.ISSUER)
        self.assertEqual(flags[flags.index('--certificate-github-workflow-repository') + 1], r.REPOSITORY)
        self.assertEqual(flags[flags.index('--certificate-github-workflow-sha') + 1], SHA)
        self.assertEqual(flags[flags.index('--certificate-identity') + 1],
                         f'https://github.com/{r.REPOSITORY}/{r.WORKFLOW}@refs/heads/main')
        self.assertFalse(any('insecure' in flag or 'regexp' in flag for flag in flags))

    def test_ambient_cosign_environment_cannot_override_trust(self):
        injected={'COSIGN_KEY':'untrusted.key','COSIGN_TRUSTED_ROOT':'untrusted.json',
                  'COSIGN_INSECURE_IGNORE_TLOG':'true','SIGSTORE_ROOT_FILE':'untrusted.json',
                  'ACTIONS_ID_TOKEN_REQUEST_URL':'https://github.example/oidc','DOCKER_CONFIG':'fixture'}
        with mock.patch.dict(os.environ,injected):
            clean=r.cosign_environment()
        self.assertFalse(any(key.startswith(('COSIGN_','SIGSTORE_')) for key in clean))
        self.assertEqual(clean['ACTIONS_ID_TOKEN_REQUEST_URL'],injected['ACTIONS_ID_TOKEN_REQUEST_URL'])
        self.assertEqual(clean['DOCKER_CONFIG'],'fixture')

    @unittest.skipUnless(shutil.which('openssl'), 'OpenSSL required for local cryptographic fixtures')
    def test_real_ephemeral_crypto_signature_and_tamper_failures(self):
        key, public, signature = [self.directory / name for name in ['fixture.key', 'fixture.pub', 'fixture.sig']]
        subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(key)],
                       check=True, capture_output=True)
        subprocess.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-out', str(public)], check=True, capture_output=True)
        blob = self.directory / 'SHA256SUMS'
        subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(key), '-out', str(signature), str(blob)],
                       check=True, capture_output=True)
        verify = ['openssl', 'dgst', '-sha256', '-verify', str(public), '-signature', str(signature), str(blob)]
        self.assertEqual(subprocess.run(verify, capture_output=True).returncode, 0)
        original = blob.read_bytes()
        blob.write_bytes(original + b'tampered')
        self.assertNotEqual(subprocess.run(verify, capture_output=True).returncode, 0)
        blob.write_bytes(original)
        signature.write_bytes(bytes([signature.read_bytes()[0] ^ 1]) + signature.read_bytes()[1:])
        self.assertNotEqual(subprocess.run(verify, capture_output=True).returncode, 0)


class GateTests(unittest.TestCase):
    def setUp(self):
        self.env = {'GITHUB_REPOSITORY': r.REPOSITORY, 'GITHUB_WORKFLOW_REF': r.REPOSITORY + '/' + r.WORKFLOW + '@refs/tags/v1.2.3',
            'GITHUB_EVENT_NAME': 'push', 'GITHUB_REF': 'refs/tags/v1.2.3', 'GITHUB_SHA': SHA,
            'GITHUB_RUN_ID': '123', 'GITHUB_RUN_ATTEMPT': '1', 'ARTEX_REF_PROTECTED': 'true', 'ARTEX_ENABLE_RELEASES': 'true'}

    def runner(self, command, **kwargs):
        if command[1] == 'ls-remote':
            output = SHA + '\trefs/tags/v1.2.3\n'
        else:
            output = SHA + '\n'
        return subprocess.CompletedProcess(command, 0, output, '')

    def gate(self, env, runner=None):
        return r.gate(env, runner or self.runner, policy_verifier=lambda ref: None)

    def test_release_requires_repository_protected_tag_optin_main_ancestry(self):
        self.assertEqual(self.gate(self.env)['mode'], 'release')
        for key, value in [('GITHUB_REPOSITORY', 'attacker/fork'), ('ARTEX_REF_PROTECTED', 'false'),
                           ('ARTEX_ENABLE_RELEASES', 'false'), ('GITHUB_EVENT_NAME', 'pull_request'),
                           ('GITHUB_REF', 'refs/heads/main'), ('GITHUB_SHA', 'bad')]:
            changed = self.env | {key: value}
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.gate(changed)
        def unrelated(command, **kwargs):
            if command[1] == 'merge-base':
                raise subprocess.CalledProcessError(1, command)
            return self.runner(command, **kwargs)
        with self.assertRaises(subprocess.CalledProcessError):
            self.gate(self.env, unrelated)

    def test_main_dispatch_can_only_dry_run_without_release_optin(self):
        env = self.env | {'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REF': 'refs/heads/main',
            'GITHUB_WORKFLOW_REF': r.REPOSITORY + '/' + r.WORKFLOW + '@refs/heads/main',
            'ARTEX_ENABLE_RELEASES': 'false', 'ARTEX_DRY_RUN': 'true'}
        self.assertEqual(self.gate(env)['mode'], 'candidate')
        with self.assertRaises(ValueError):
            self.gate(env | {'ARTEX_DRY_RUN': 'false'})

    def test_release_fails_closed_when_active_tag_policy_is_not_verified(self):
        with mock.patch.object(r, 'run'):
            with self.assertRaisesRegex(ValueError, 'policy inactive'):
                r.gate(self.env, self.runner, policy_verifier=mock.Mock(side_effect=ValueError('policy inactive')))

    def test_workflow_permissions_pins_and_publication_structure(self):
        workflow = (Path(__file__).resolve().parent.parent / '.github/workflows/release.yml').read_text()
        self.assertNotIn('type=raw,value=latest', workflow)
        self.assertNotIn('--clobber', workflow)
        self.assertIn('workflow_dispatch:', workflow)
        self.assertIn('cosign-release: v3.1.4', workflow)
        self.assertIn('retention-days: 7', workflow)
        for line in workflow.splitlines():
            if 'uses:' in line:
                used=line.split('uses:',1)[1].strip()
                if used in ('./.github/workflows/ci.yml','./.github/workflows/image-security.yml'):
                    continue
                self.assertRegex(line, r'uses: [A-Za-z0-9_./-]+@[a-f0-9]{40}(?:\s|$)')
        self.assertIn('needs: [policy, preflight-tests, preflight-images]',workflow)
        self.assertEqual(workflow.count('id-token: write'), 2)
        self.assertEqual(workflow.count('contents: write'), 1)
        dry = workflow.split('  dry-run:', 1)[1].split('  frontend:', 1)[0]
        self.assertNotIn('packages: write', dry)
        self.assertNotIn('contents: write', dry)
        self.assertIn('127.0.0.1:5000:5000', dry)


if __name__ == '__main__':
    unittest.main()

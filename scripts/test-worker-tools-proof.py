#!/usr/bin/env python3
"""Exercise host-owned worker proof bindings without Docker or source-tree caches."""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
DRIVER = ROOT / 'scripts/test-worker-tools.py'
IMAGE_ID = 'sha256:' + 'a' * 64
LAYERS = ['sha256:' + 'b' * 64, 'sha256:' + 'c' * 64]


def load_driver():
    # Execute the actual driver without importing it through a loader that writes
    # bytecode into source. Its main is called only inside the mocked boundary.
    module = types.ModuleType('worker_tools_proof_driver')
    module.__file__ = str(DRIVER)
    exec(compile(DRIVER.read_bytes(), str(DRIVER), 'exec'), module.__dict__)
    return module


class WorkerToolsProofTests(unittest.TestCase):
    def setUp(self):
        self.driver = load_driver()
        self.expected = self.driver.EXPECTED_CHECK_COUNT
        self.checks = ['Owned synthetic check ' + str(index) for index in range(self.expected)]
        self.temporary = tempfile.TemporaryDirectory(prefix='artex-worker-tools-proof-')
        self.addCleanup(self.temporary.cleanup)
        self.output = Path(self.temporary.name) / 'new-report-directory' / 'proof.json'
        self.calls = []
        self.stdout = io.StringIO()

    def invoke(self, report=None, *, raw=None, container_error=False):
        container_stdout = raw if raw is not None else json.dumps(report)
        inspected = [{'Id': IMAGE_ID, 'Os': 'linux', 'Architecture': 'amd64',
                      'RootFS': {'Layers': LAYERS}}]

        def fake_run(arguments, **options):
            self.calls.append(list(arguments))
            if arguments[:3] == ['docker', 'image', 'inspect']:
                return subprocess.CompletedProcess(arguments, 0, json.dumps(inspected), '')
            if arguments[:2] == ['docker', 'run']:
                if container_error:
                    raise subprocess.CalledProcessError(1, arguments, output='', stderr='Owned fixture failed')
                return subprocess.CompletedProcess(arguments, 0, container_stdout, '')
            if arguments[:3] == ['docker', 'rm', '-f']:
                return subprocess.CompletedProcess(arguments, 0, '', '')
            raise AssertionError('Unexpected non-mocked operation: ' + str(arguments[:3]))

        arguments = [str(DRIVER), '--image', 'owned-fixture:worker', '--report', str(self.output)]
        with patch.object(self.driver.subprocess, 'run', side_effect=fake_run), \
                patch.object(sys, 'argv', arguments), contextlib.redirect_stdout(self.stdout):
            self.driver.main()

    def assert_cleanup_attempted(self):
        runs = [call for call in self.calls if call[:2] == ['docker', 'run']]
        removals = [call for call in self.calls if call[:3] == ['docker', 'rm', '-f']]
        self.assertEqual(len(runs), 1)
        self.assertEqual(len(removals), 1)
        name = runs[0][runs[0].index('--name') + 1]
        self.assertEqual(removals[0], ['docker', 'rm', '-f', name])
        self.assertIn(IMAGE_ID, runs[0])
        self.assertNotIn('owned-fixture:worker', runs[0])

    def assert_rejected(self, report=None, *, raw=None, exception=ValueError):
        with self.assertRaises(exception):
            self.invoke(report, raw=raw)
        self.assertFalse(self.output.exists(), 'Invalid container output wrote a success proof')
        self.assertEqual(self.stdout.getvalue(), '', 'Invalid container output printed a success proof')
        self.assert_cleanup_attempted()

    def test_container_cannot_overwrite_host_authenticated_bindings(self):
        for key, value in {
            'image_id': 'sha256:' + 'f' * 64,
            'platform': 'linux/forged',
            'rootfs_diff_ids': ['sha256:' + 'e' * 64],
            'source_sha256': {'forged-source': 'forged-hash'},
            'schema_version': 999,
        }.items():
            with self.subTest(key=key):
                self.calls.clear()
                self.assert_rejected({'passed': True, 'checks': self.checks, key: value})

    def test_malformed_or_incomplete_success_results_are_rejected(self):
        invalid = [None, [], True, {}, {'passed': True}, {'checks': self.checks}]
        invalid += [{'passed': value, 'checks': self.checks}
                    for value in [False, None, 0, 1, 'true']]
        invalid += [{'passed': True, 'checks': value}
                    for value in [None, {}, 'x' * self.expected, self.checks[:-1], self.checks + ['Extra']]]
        invalid += [{'passed': True, 'checks': [value] + self.checks[1:]}
                    for value in ['', None, True, 1, {}]]
        for number, report in enumerate(invalid):
            with self.subTest(case=number):
                self.calls.clear()
                self.assert_rejected(report)
        with self.subTest(case='non-JSON output'):
            self.calls.clear()
            self.assert_rejected(raw='Owned fixture output is not JSON')

    def test_container_execution_failure_does_not_publish_success(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke(container_error=True)
        self.assertFalse(self.output.exists())
        self.assertEqual(self.stdout.getvalue(), '')
        self.assert_cleanup_attempted()

    def test_valid_output_retains_inspected_identity_sources_and_cleanup(self):
        self.invoke({'passed': True, 'checks': self.checks})
        proof = json.loads(self.output.read_text())
        self.assertEqual(proof, json.loads(self.stdout.getvalue()))
        self.assertIs(proof['passed'], True)
        self.assertEqual(proof['checks'], self.checks)
        self.assertEqual(proof['schema_version'], 1)
        self.assertEqual(proof['image_id'], IMAGE_ID)
        self.assertEqual(proof['platform'], 'linux/amd64')
        self.assertEqual(proof['rootfs_diff_ids'], LAYERS)
        bindings = proof['source_sha256']
        self.assertIn('scripts/test-worker-tools.py', bindings)
        self.assertIn('tools/jq/test-behavior.py', bindings)
        self.assertIn('tools/util-linux/test-behavior.py', bindings)
        for filename, digest in bindings.items():
            source = Path(filename)
            self.assertFalse(source.is_absolute())
            self.assertNotIn('..', source.parts)
            self.assertEqual(digest, hashlib.sha256((ROOT / source).read_bytes()).hexdigest(), filename)
        self.assert_cleanup_attempted()


if __name__ == '__main__':
    unittest.main()

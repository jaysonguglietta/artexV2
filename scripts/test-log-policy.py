#!/usr/bin/env python3
"""Regression tests for non-destructive Docker/journald configuration merging."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('log_policy', Path(__file__).with_name('configure-docker-logs.py'))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


class LogPolicyTests(unittest.TestCase):
    def test_preserves_unrelated_daemon_settings_and_replaces_driver_specific_options(self):
        merged = json.loads(policy.merged_daemon('{"storage-driver":"overlay2","insecure-registries":["fixture.invalid:5000"],"log-driver":"json-file","log-opts":{"labels":"old-driver-option"}}'))
        self.assertEqual(merged['storage-driver'], 'overlay2')
        self.assertEqual(merged['insecure-registries'], ['fixture.invalid:5000'])
        self.assertEqual(merged['log-driver'], 'local')
        self.assertEqual(merged['log-opts'], {'max-size': '10m', 'max-file': '3', 'compress': 'true'})

    def test_malformed_or_ambiguous_existing_config_is_never_replaced(self):
        for raw in ['{', '[]', '{"log-driver":"local","log-driver":"json-file"}']:
            with self.assertRaises(ValueError): policy.merged_daemon(raw)

    def test_configuration_update_retains_private_original_and_is_idempotent(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'daemon.json'
            original = '{"debug":false}'
            path.write_text(original)
            updated = policy.merged_daemon(original)
            self.assertTrue(policy.install(path, updated))
            backups = list(path.parent.glob('daemon.json.before-artex-*'))
            self.assertEqual(len(backups), 1)
            self.assertEqual(backups[0].read_text(), original)
            self.assertEqual(backups[0].stat().st_mode & 0o777, 0o600)
            self.assertFalse(policy.install(path, updated))
            self.assertEqual(len(list(path.parent.glob('daemon.json.before-artex-*'))), 1)

    def test_symlink_target_is_not_followed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            target = root / 'unrelated'
            target.write_text('keep')
            link = root / 'daemon.json'
            link.symlink_to(target)
            with self.assertRaises(ValueError): policy.install(link, '{}')
            self.assertEqual(target.read_text(), 'keep')


if __name__ == '__main__': unittest.main()

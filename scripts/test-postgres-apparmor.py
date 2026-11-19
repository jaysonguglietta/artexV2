#!/usr/bin/env python3
"""Check the exact PostgreSQL AppArmor allowlist without mounting filesystems."""
import ast
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[1]
ACCEPTED = {"docker-default (enforce)", "docker-default//&unconfined (enforce)"}


class PostgresAppArmorTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = (ROOT / 'postgres/bounded-entrypoint.sh').read_text()
        start = source.index('postgres_profile_is_enforcing() {\n')
        finish = source.index('\n}\n', start) + 3
        # Execute only the pure validator, never the mount/bootstrap entrypoint.
        cls.function = source[start:finish]

    def accepted(self, label):
        result = subprocess.run(['sh', '-c', 'set -eu\n' + self.function + '\npostgres_profile_is_enforcing "$1"', 'profile-test', label],
                                capture_output=True, text=True, timeout=5)
        return result.returncode == 0

    def test_only_reviewed_enforcing_labels_are_accepted(self):
        for label in ACCEPTED:
            with self.subTest(label=label): self.assertTrue(self.accepted(label))

    def test_unconfined_complain_unknown_stacks_and_variations_are_rejected(self):
        rejected = ['', 'unconfined', 'unconfined (enforce)', 'docker-default (complain)',
                    'docker-default//&unconfined (complain)', 'docker-default//&unconfined (mixed)',
                    'docker-default//&unknown (enforce)', 'docker-default//&unconfined//&unknown (enforce)',
                    'unconfined//&docker-default (enforce)', ':namespace://docker-default (enforce)',
                    'prefix-docker-default (enforce)', 'docker-default (enforce)-suffix',
                    ' docker-default (enforce)', 'docker-default (enforce) ', 'docker-default (enforce)\n']
        for label in rejected:
            with self.subTest(label=label): self.assertFalse(self.accepted(label))

    def test_live_assertions_use_the_same_exact_allowlist(self):
        for name in ['scripts/test-compose.py', 'scripts/test-pgdisk.py']:
            tree = ast.parse((ROOT / name).read_text())
            values = [ast.literal_eval(node.value) for node in tree.body if isinstance(node, ast.Assign)
                      and any(isinstance(target, ast.Name) and target.id == 'POSTGRES_APPARMOR_LABELS' for target in node.targets)]
            self.assertEqual(len(values), 1, name)
            self.assertEqual(set(values[0]), ACCEPTED, name)
            self.assertEqual(len(values[0]), 2, name)

    def test_shell_read_retains_valid_label_without_terminal_newline(self):
        command = 'set -eu\n' + self.function + '''
current_label=''
if IFS= read -r current_label; then :; elif [ -z "$current_label" ]; then exit 1; fi
postgres_profile_is_enforcing "$current_label"
'''
        for label in ACCEPTED:
            result = subprocess.run(['sh', '-c', command], input=label, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, label)


if __name__ == '__main__': unittest.main()

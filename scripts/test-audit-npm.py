#!/usr/bin/env python3
"""Exercise active, withdrawn, cyclic, and unknown dependency paths."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('audit_npm', Path(__file__).with_name('audit-npm.py'))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)
URL = 'https://github.com/advisories/GHSA-test-test-test'

class AdvisoryTests(unittest.TestCase):
    def test_withdrawn_leaf_resolves_transitive_chain(self):
        report = {'vulnerabilities': {'parent': {'via': ['leaf']}, 'leaf': {'via': [{'url': URL}]}}}
        self.assertEqual(audit.classify(report, {URL: '2026-11-18'}), ([], [URL]))

    def test_active_leaf_blocks_parent(self):
        report = {'vulnerabilities': {'parent': {'via': ['leaf']}, 'leaf': {'via': [{'url': URL}]}}}
        self.assertEqual(audit.classify(report, {})[0], [URL])

    def test_missing_and_cyclic_evidence_fail_closed(self):
        for via in [['missing'], ['parent'], []]:
            self.assertTrue(audit.classify({'vulnerabilities': {'parent': {'via': via}}}, {})[0])

    def test_withdrawal_does_not_hide_other_advisories(self):
        report = {'vulnerabilities': {'leaf': {'via': [{'url': URL}, {'url': 'https://example.test/active'}]}}}
        self.assertEqual(audit.classify(report, {URL: '2026-11-18'})[0], ['https://example.test/active'])

if __name__ == '__main__':
    unittest.main()

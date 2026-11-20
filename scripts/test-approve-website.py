#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('approval', Path(__file__).with_name('approve-website.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class ApprovalTests(unittest.TestCase):
    def test_preserves_other_tasks_and_denies(self):
        policy = {'sessions': {'task-2': ['8.8.8.8/32']}, 'management_cidrs': ['10.0.0.0/8'], 'dns_servers': ['1.1.1.1']}
        result = module.updated_policy(policy, 1, ['141.157.239.219'])
        self.assertEqual(result['sessions']['task-1'], ['141.157.239.219/32'])
        self.assertEqual(result['sessions']['task-2'], policy['sessions']['task-2'])
        self.assertEqual(result['management_cidrs'], policy['management_cidrs'])
        self.assertNotIn('task-1', policy['sessions'])

    def test_refuses_private_and_denied_addresses(self):
        for address in ['127.0.0.1', '169.254.169.254', '10.1.1.1', '141.157.239.219']:
            with self.subTest(address=address), self.assertRaises(ValueError):
                module.updated_policy({'management_cidrs':['141.157.239.0/24']},1,[address])

    def test_rejects_url_credentials_and_non_https(self):
        for url in ['http://example.com', 'https://user:pass@example.com', 'https://example.com:8443']:
            with self.assertRaises(ValueError): module.target_host(url)

if __name__ == '__main__': unittest.main()

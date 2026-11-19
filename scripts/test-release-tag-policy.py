#!/usr/bin/env python3
"""No-network regressions for narrow release tag policy and safe idempotence."""
import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('release_tag_policy', Path(__file__).with_name('release-tag-policy.py'))
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)


def live(value, identity):
    return copy.deepcopy(value) | {'id': identity, 'source': p.REPOSITORY, 'source_type': 'Repository'}


class FakeGitHub:
    def __init__(self, managed=True):
        self.items = [live(value, index + 1) for index, value in enumerate(p.expected_policy())] if managed else []
        self.items.append({'id': 99, 'name': 'Existing unrelated main rules', 'target': 'branch'})
        self.writes = []
        self.actor = {'login': p.OWNER, 'id': p.OWNER_ID}

    def request(self, path, body=None):
        if body is not None:
            self.writes.append((path, copy.deepcopy(body)))
            item = live(body, len(self.items) + 100)
            self.items.append(item)
            return item
        if path == '/user':
            return self.actor
        if path == '/repos/' + p.REPOSITORY:
            return {'full_name': p.REPOSITORY, 'visibility': 'public', 'permissions': {'admin': True}}
        if '/rulesets?' in path:
            return self.items
        identity = int(path.split('/rulesets/')[1].split('?')[0])
        return next(item for item in self.items if item['id'] == identity)


class TagPolicyTests(unittest.TestCase):
    def test_separate_creation_bypass_cannot_bypass_immutability(self):
        immutable, creation = p.expected_policy()
        self.assertEqual(immutable['bypass_actors'], [])
        self.assertEqual({rule['type'] for rule in immutable['rules']}, {'update', 'deletion'})
        self.assertEqual(creation['rules'], [{'type': 'creation'}])
        self.assertEqual(creation['bypass_actors'], [{'actor_id': p.OWNER_ID, 'actor_type': 'User', 'bypass_mode': 'always'}])
        self.assertEqual(p.verify(FakeGitHub())['status'], 'active')

    def test_inactive_or_wrong_target_or_excluded_refs_fail_closed(self):
        for field, bad in [('enforcement', 'disabled'), ('enforcement', 'evaluate'), ('target', 'branch'),
                           ('source', 'attacker/fork'),
                           ('conditions', {'ref_name': {'include': ['refs/tags/v*'], 'exclude': ['refs/tags/v1*']}}),
                           ('conditions', {'ref_name': {'include': ['refs/heads/v*'], 'exclude': []}})]:
            api = FakeGitHub()
            api.items[0][field] = bad
            with self.subTest(field=field, bad=bad), self.assertRaises(ValueError):
                p.verify(api)

    def test_missing_or_duplicate_or_weakened_protection_is_refused(self):
        for mutate in [lambda api: api.items.pop(0), lambda api: api.items.append(copy.deepcopy(api.items[0])),
                       lambda api: api.items[0].update(rules=[{'type': 'update'}]),
                       lambda api: api.items[0].update(bypass_actors=copy.deepcopy(api.items[1]['bypass_actors'])),
                       lambda api: api.items[1].update(bypass_actors=[{'actor_id': 1, 'actor_type': 'User', 'bypass_mode': 'always'}]),
                       lambda api: api.items[1].update(bypass_actors=[{'actor_id': p.OWNER_ID, 'actor_type': 'User', 'bypass_mode': 'exempt'}])]:
            api = FakeGitHub()
            mutate(api)
            with self.assertRaises(ValueError):
                p.verify(api)

    def test_runtime_readonly_visibility_does_not_claim_hidden_bypass_verification(self):
        api = FakeGitHub()
        for item in api.items[:2]:
            del item['bypass_actors']
        result = p.verify(api, runtime=True)
        self.assertTrue(all(item['bypass_policy'] == 'hidden_by_github' for item in result['rulesets']))
        with self.assertRaisesRegex(ValueError, 'hidden'):
            p.verify(api)

    def test_apply_only_adds_owned_policies_and_second_apply_is_readonly(self):
        api = FakeGitHub(managed=False)
        original = copy.deepcopy(api.items[0])
        self.assertEqual(p.apply(api)['status'], 'active')
        self.assertEqual(len(api.writes), 2)
        self.assertTrue(all(path == '/repos/' + p.REPOSITORY + '/rulesets' for path, _ in api.writes))
        self.assertIn(original, api.items)
        p.apply(api)
        self.assertEqual(len(api.writes), 2)

    def test_apply_refuses_wrong_owner_or_existing_policy_conflict_before_any_write(self):
        api = FakeGitHub(managed=False)
        api.actor = {'login': 'other-account', 'id': 1}
        with self.assertRaises(ValueError):
            p.apply(api)
        self.assertEqual(api.writes, [])
        api = FakeGitHub()
        api.items[1]['enforcement'] = 'disabled'
        with self.assertRaises(ValueError):
            p.apply(api)
        self.assertEqual(api.writes, [])

    def test_ref_selection_and_api_redirects_are_fail_closed(self):
        for ref in ['refs/heads/main', 'refs/tags/latest', 'refs/tags/v1.2.3/extra', 'refs/tags/v1.2.3\n']:
            with self.assertRaises(ValueError):
                p.verify(FakeGitHub(), ref)
        with self.assertRaises(ValueError):
            p.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://attacker.invalid/')


if __name__ == '__main__':
    unittest.main()

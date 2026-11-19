#!/usr/bin/env python3
"""Plan/apply two additive release-tag rulesets, or verify their active policy.

This tool never creates tags/releases, enables publication, modifies other
rulesets or switches the global gh account. Personal credentials stay in memory.
"""
import argparse
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request

REPOSITORY = 'jaysonguglietta/artexV2'
OWNER = 'jaysonguglietta'
OWNER_ID = 15835881
PATTERN = 'refs/tags/v*'
REF = re.compile(r'refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?\Z')
MAX_JSON = 1 << 20
NAMES = ('ARTEX release tags: immutable', 'ARTEX release tags: owner creation')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def expected_policy():
    common = {'target': 'tag', 'enforcement': 'active',
              'conditions': {'ref_name': {'include': [PATTERN], 'exclude': []}}}
    return [common | {'name': NAMES[0], 'bypass_actors': [],
                      'rules': [{'type': 'update', 'parameters': {'update_allows_fetch_and_merge': False}},
                                {'type': 'deletion'}]},
            common | {'name': NAMES[1], 'rules': [{'type': 'creation'}],
                      'bypass_actors': [{'actor_id': OWNER_ID, 'actor_type': 'User', 'bypass_mode': 'always'}]}]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        raise ValueError('GitHub policy API redirects are prohibited')


class GitHub:
    def __init__(self, token=None):
        self.token = token
        self.opener = urllib.request.build_opener(NoRedirect())

    def request(self, path, body=None):
        require(path == '/user' or path.startswith('/repos/' + REPOSITORY + '/rulesets')
                or path == '/repos/' + REPOSITORY, 'Untrusted tag-policy API path')
        require('..' not in path and '#' not in path, 'Invalid tag-policy API path')
        headers = {'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28',
                   'User-Agent': 'artex-release-tag-policy'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        raw = None if body is None else json.dumps(body).encode()
        if raw is not None:
            headers['Content-Type'] = 'application/json'
        request = urllib.request.Request('https://api.github.com' + path, data=raw, headers=headers,
                                         method='GET' if body is None else 'POST')
        try:
            with self.opener.open(request, timeout=20) as response:
                data = response.read(MAX_JSON + 1)
        except urllib.error.HTTPError as error:
            code = error.code
            error.close()
            raise ValueError('Tag-policy API failed closed (HTTP ' + str(code) + '); no alternate account/bypass is selected') from None
        require(0 < len(data) <= MAX_JSON, 'Tag-policy API JSON exceeds its bound')
        return json.loads(data)


def all_rulesets(api):
    output = []
    for page in range(1, 11):
        values = api.request('/repos/' + REPOSITORY + '/rulesets?includes_parents=true&per_page=100&page=' + str(page))
        require(isinstance(values, list) and len(values) <= 100, 'Invalid ruleset inventory')
        output.extend(values)
        if len(values) < 100:
            return output
    raise ValueError('Ruleset inventory exceeds the bounded review limit')


def validate_ruleset(value, expected, runtime=False):
    require(isinstance(value, dict) and type(value.get('id')) is int and value['id'] > 0,
            'Missing ruleset identity')
    for field in ('name', 'target', 'enforcement', 'conditions'):
        require(value.get(field) == expected[field], 'Release tag policy mismatch: ' + field)
    require(value.get('source_type') == 'Repository' and value.get('source') == REPOSITORY,
            'Release tag policy has an unexpected source')
    rules = value.get('rules')
    require(isinstance(rules, list) and len(rules) == len(expected['rules']), 'Release tag rules are missing or ambiguous')
    found = set()
    for rule in rules:
        require(isinstance(rule, dict) and set(rule) <= {'type', 'parameters'}, 'Unexpected tag rule fields')
        kind = rule.get('type')
        require(isinstance(kind, str) and kind not in found, 'Duplicate/invalid tag rule')
        found.add(kind)
        parameters = rule.get('parameters')
        require(parameters in (None, {}) or kind == 'update' and isinstance(parameters, dict)
                and set(parameters) == {'update_allows_fetch_and_merge'} and parameters['update_allows_fetch_and_merge'] is False,
                'Tag update bypass parameters are not permitted')
    require(found == {rule['type'] for rule in expected['rules']}, 'Required tag protection is absent')
    visible = 'bypass_actors' in value
    # GitHub hides bypass actors from tokens without ruleset write access.
    # Runtime checks have read-only permissions; full owner verification is
    # mandatory during apply/maintenance and validates every bypass exactly.
    require(visible or runtime, 'Bypass policy hidden; verify using the personal owner account')
    if visible:
        require(value['bypass_actors'] == expected['bypass_actors'], 'Release tag bypass policy differs from the reviewed policy')
    return {'id': value['id'], 'name': value['name'], 'bypass_policy': 'verified' if visible else 'hidden_by_github'}


def verify(api, ref='refs/tags/v0.0.0-policy-check', runtime=False):
    require(isinstance(ref, str) and REF.fullmatch(ref), 'An exact semantic version ref is required')
    inventory = all_rulesets(api)
    result = []
    for expected in expected_policy():
        matching = [value for value in inventory if isinstance(value, dict) and value.get('name') == expected['name']]
        require(len(matching) == 1, 'Required release tag ruleset missing or duplicated: ' + expected['name'])
        identity = matching[0].get('id')
        require(type(identity) is int and identity > 0, 'Invalid tag ruleset ID')
        value = api.request('/repos/' + REPOSITORY + '/rulesets/' + str(identity) + '?includes_parents=true')
        require(value.get('id') == identity, 'Ruleset changed identity during verification')
        result.append(validate_ruleset(value, expected, runtime))
    return {'repository': REPOSITORY, 'ref': ref, 'status': 'active', 'rulesets': result,
            'verification': 'REST active tag targets and exact full-ref conditions; no tag was created or mutated'}


def verify_runtime(ref, token=None):
    return verify(GitHub(token), ref, runtime=True)


def apply(api):
    actor = api.request('/user')
    repository = api.request('/repos/' + REPOSITORY)
    require(actor.get('login') == OWNER and actor.get('id') == OWNER_ID, 'Tag policy mutation requires the personal repository owner')
    require(repository.get('full_name') == REPOSITORY and repository.get('visibility') == 'public'
            and repository.get('permissions', {}).get('admin') is True, 'Public repository administrator permission is required')
    inventory = all_rulesets(api)
    # Validate every existing managed name before any POST. Other names/settings
    # are preserved; mismatched managed policies require explicit manual review.
    missing = []
    for expected in expected_policy():
        matching = [value for value in inventory if isinstance(value, dict) and value.get('name') == expected['name']]
        require(len(matching) <= 1, 'Duplicate managed tag policy; no mutation attempted')
        if matching:
            detail = api.request('/repos/' + REPOSITORY + '/rulesets/' + str(matching[0]['id']) + '?includes_parents=true')
            validate_ruleset(detail, expected)
        else:
            missing.append(expected)
    for expected in missing:
        created = api.request('/repos/' + REPOSITORY + '/rulesets', expected)
        validate_ruleset(created, expected)
    return verify(api)


def personal_token():
    result = subprocess.run(['gh', 'auth', 'token', '--hostname', 'github.com', '--user', OWNER],
                            capture_output=True, text=True, timeout=20)
    require(result.returncode == 0 and result.stdout.strip(), 'Personal GitHub credential lookup failed')
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['plan', 'apply', 'verify'])
    parser.add_argument('--personal-account', action='store_true', help='Read only the named personal gh credential into this process')
    parser.add_argument('--runtime', action='store_true', help='Read-only Actions check; GitHub may hide bypass actors')
    parser.add_argument('--ref', default='refs/tags/v0.0.0-policy-check')
    args = parser.parse_args()
    if args.operation == 'plan':
        print(json.dumps({'repository': REPOSITORY, 'rulesets': expected_policy()}, indent=2))
        return
    require(not args.runtime or args.operation == 'verify', 'Runtime mode never modifies settings')
    token = personal_token() if args.personal_account else os.environ.get('GH_TOKEN') or os.environ.get('GITHUB_TOKEN')
    api = GitHub(token)
    if args.operation == 'apply':
        require(args.personal_account, 'Apply requires explicit personal-account selection')
        result = apply(api)
    else:
        result = verify(api, args.ref, args.runtime)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print('Release tag policy refused: ' + str(error), file=sys.stderr)
        sys.exit(1)

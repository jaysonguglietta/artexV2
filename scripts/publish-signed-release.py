#!/usr/bin/env python3
"""Publish a fully verified, new version; never overwrite an existing release."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('signed_release', Path(__file__).with_name('signed-release.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        raise ValueError('GitHub API redirects are prohibited')


def publish(directory):
    ctx = r.gate()
    r.require(ctx['mode'] == 'release', 'Candidate publication is prohibited')
    r.verify(directory, ctx)
    tag = ctx['ref'][10:]
    request = urllib.request.Request(f'https://api.github.com/repos/{r.REPOSITORY}/releases/tags/{tag}',
        headers={'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
                 'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28'})
    try:
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=20):
            raise ValueError('Refusing to replace an existing release, draft or its assets')
    except urllib.error.HTTPError as error:
        code=error.code
        error.close()
        r.require(code == 404, 'Release existence check failed closed')
    # Draft-first supports GitHub immutable releases when the operator enables
    # that repository setting. No tag is created and no asset is overwritten.
    command = ['gh', 'release', 'create', tag, '--repo', r.REPOSITORY, '--verify-tag',
               '--draft', '--latest=false', '--title', tag, '--generate-notes']
    if '-' in tag:
        command.append('--prerelease')
    r.run(command)
    r.run(['gh', 'release', 'upload', tag, '--repo', r.REPOSITORY,
           *[str(path) for path in sorted(directory.iterdir())]])
    r.run(['gh', 'release', 'edit', tag, '--repo', r.REPOSITORY, '--draft=false', '--latest=false'])


if __name__ == '__main__':
    try:
        r.require(len(sys.argv) == 2, 'Usage: publish-signed-release.py VERIFIED_DIRECTORY')
        publish(Path(sys.argv[1]).resolve())
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print('Release publication refused: ' + str(error), file=sys.stderr)
        sys.exit(1)

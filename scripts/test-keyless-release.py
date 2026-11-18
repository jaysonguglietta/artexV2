#!/usr/bin/env python3
"""Negative checks against REAL keyless bundles in the main-only CI dry-run.

No alternate identity/issuer override is exposed by the production verifier.
"""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

spec = importlib.util.spec_from_file_location('signed_release', Path(__file__).with_name('signed-release.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


def refused(command, label):
    result = subprocess.run(command, capture_output=True, text=True, timeout=240, env=r.cosign_environment())
    if result.returncode == 0:
        raise RuntimeError('Negative verification unexpectedly accepted: ' + label)
    print('PASS real keyless rejection: ' + label)


def main():
    directory = Path(sys.argv[1]).resolve()
    ctx = r.context('candidate', 'refs/heads/main', os.environ['GITHUB_SHA'],
                    os.environ['GITHUB_RUN_ID'], os.environ['GITHUB_RUN_ATTEMPT'])
    r.verify(directory, ctx)
    command = ['cosign', 'verify-blob', '--bundle', str(directory / 'SHA256SUMS.sigstore.json'),
               *r.certificate_flags(ctx), str(directory / 'SHA256SUMS')]
    for flag, value, label in [
        ('--certificate-identity', 'https://github.com/attacker/untrusted/.github/workflows/release.yml@refs/heads/main', 'wrong repository/workflow identity'),
        ('--certificate-identity', 'https://github.com/' + r.REPOSITORY + '/.github/workflows/other.yml@refs/heads/main', 'wrong workflow'),
        ('--certificate-oidc-issuer', 'https://accounts.google.com', 'wrong issuer'),
        ('--certificate-github-workflow-sha', '0' * 40, 'wrong commit'),
        ('--certificate-github-workflow-ref', 'refs/tags/v0.0.0', 'wrong ref')]:
        negative = command.copy()
        negative[negative.index(flag) + 1] = value
        refused(negative, label)
    with tempfile.TemporaryDirectory(prefix='artex-keyless-negatives-') as tmp:
        tmp = Path(tmp)
        altered = tmp / 'tampered-checksums'
        altered.write_bytes((directory / 'SHA256SUMS').read_bytes() + b'tampered\n')
        refused(command[:-1] + [str(altered)], 'tampered signed checksums')
        fixture = tmp / 'modified-artifact'
        shutil.copytree(directory, fixture)
        archive = fixture / r.archive_names(ctx)[0]
        archive.write_bytes(archive.read_bytes() + b'tamper')
        try:
            r.verify(fixture, ctx)
        except (ValueError, subprocess.SubprocessError):
            print('PASS real keyless rejection: tampered archive')
        else:
            raise RuntimeError('Tampered archive accepted')
        # Even correctly signed metadata cannot choose another repository. This
        # uses a fresh real OIDC signature, not a fake bundle or mocked verifier.
        fixture = tmp / 'wrong-provenance'
        shutil.copytree(directory, fixture)
        path = fixture / 'provenance.json'
        data = json.loads(path.read_text())
        data['predicate']['buildDefinition']['externalParameters']['repository'] = 'attacker/untrusted'
        path.write_text(json.dumps(data) + '\n')
        subprocess.run(['cosign', 'sign-blob', '--yes', '--oidc-provider=github-actions',
                        '--bundle', str(fixture / 'provenance.json.sigstore.json'), str(path)], check=True, timeout=240, env=r.cosign_environment())
        try:
            r.verify(fixture, ctx)
        except (ValueError, subprocess.SubprocessError):
            print('PASS real keyless rejection: valid signature with wrong repository provenance')
        else:
            raise RuntimeError('Wrong repository provenance accepted')
    print('Real keyless identity/issuer/ref/SHA, tamper and provenance negative checks passed')


if __name__ == '__main__':
    main()

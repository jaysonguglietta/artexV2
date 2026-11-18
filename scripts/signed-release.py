#!/usr/bin/env python3
"""Create and verify ARTEX release metadata using fixed GitHub trust identities.

No artifact may choose its trusted repository, workflow, issuer, tag or commit.
Candidate mode is explicitly separate and cannot verify as a production release.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import sys
import zipfile

REPOSITORY = 'jaysonguglietta/artexV2'
WORKFLOW = '.github/workflows/release.yml'
ISSUER = 'https://token.actions.githubusercontent.com'
COSIGN_VERSION = 'v3.1.4'
IMAGE_ROOT = 'ghcr.io/jaysonguglietta/artexv2'
CANDIDATE_IMAGE = 'localhost:5000/artex-signing-candidate'
BUILD_TYPE = 'https://github.com/' + REPOSITORY + '/release-build/v1'
PREDICATE = 'https://slsa.dev/provenance/v1'
TAG = re.compile(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?\Z')
COMMIT = re.compile(r'[0-9a-f]{40}\Z')
DIGEST = re.compile(r'sha256:[0-9a-f]{64}\Z')
TARGETS = ('linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64', 'windows-amd64')
MAX_METADATA = 4 << 20
MAX_ARCHIVE = 1 << 30
MAX_EXPANDED = 2 << 30
MAX_FILES = 20000


def require(condition, message):
    if not condition:
        raise ValueError(message)


def run(command, **kwargs):
    return subprocess.run(command, check=True, timeout=240, **kwargs)


def context(mode, ref, commit, run_id, attempt):
    require(COMMIT.fullmatch(commit) is not None, 'Expected an exact lowercase 40-character commit SHA')
    require(str(run_id).isdigit() and int(run_id) > 0, 'Invalid workflow run ID')
    require(str(attempt).isdigit() and int(attempt) > 0, 'Invalid workflow attempt')
    if mode == 'release':
        require(ref.startswith('refs/tags/') and TAG.fullmatch(ref[10:]), 'Expected an explicit version tag')
    else:
        require(mode == 'candidate' and ref == 'refs/heads/main', 'Candidates must come from main')
    return {'mode': mode, 'repository': REPOSITORY, 'workflow': WORKFLOW, 'ref': ref,
            'commit': commit, 'run_id': str(run_id), 'run_attempt': str(attempt)}


def certificate_flags(ctx):
    # No regexp, artifact-derived identity, insecure flags or custom trusted roots.
    return ['--certificate-identity', f'https://github.com/{REPOSITORY}/{WORKFLOW}@{ctx["ref"]}',
            '--certificate-oidc-issuer', ISSUER,
            '--certificate-github-workflow-repository', REPOSITORY,
            '--certificate-github-workflow-ref', ctx['ref'],
            '--certificate-github-workflow-sha', ctx['commit'],
            '--certificate-github-workflow-trigger', 'push' if ctx['mode'] == 'release' else 'workflow_dispatch']


def regular(directory, name, maximum=MAX_METADATA):
    require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,180}', name) is not None,
            'Unsafe release filename')
    path = directory / name
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_size <= maximum, 'Not a bounded regular release file')
    return path


def load_json(path):
    require(path.stat().st_size <= MAX_METADATA, 'Metadata exceeds size limit')
    def unique(pairs):
        output = {}
        for key, value in pairs:
            require(key not in output, 'Duplicate JSON key')
            output[key] = value
        return output
    return json.loads(path.read_text(), object_pairs_hook=unique)


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            digest.update(chunk)
    return digest.hexdigest()


def archive_names(ctx):
    if ctx['mode'] == 'candidate':
        return [f'artex-signing-candidate-{ctx["run_id"]}-{ctx["run_attempt"]}.zip']
    return [f'artex-{ctx["ref"][11:]}-{target}.zip' for target in TARGETS]


def check_zip(path):
    # Verify only; never extract attacker-controlled members as a side effect.
    total, seen = 0, set()
    with zipfile.ZipFile(path) as archive:
        members = archive.infolist()
        require(0 < len(members) <= MAX_FILES, 'Archive entry count exceeded')
        for entry in members:
            name = entry.filename.rstrip('/')
            parts = PurePosixPath(name).parts
            mode = entry.external_attr >> 16
            require(name and len(name.encode()) <= 1024 and len(parts) <= 32
                    and '\\' not in name and '\x00' not in name and ':' not in name
                    and not name.startswith('/') and all(part not in ('', '.', '..') for part in name.split('/'))
                    and name not in seen, 'Unsafe or duplicate archive member')
            require(not stat.S_ISLNK(mode) and (stat.S_IFMT(mode) in (0, stat.S_IFREG, stat.S_IFDIR)),
                    'Archive links/special files are prohibited')
            require(not entry.flag_bits & 1, 'Encrypted release archive prohibited')
            seen.add(name)
            total += entry.file_size
            require(total <= MAX_EXPANDED, 'Archive expansion budget exceeded')
        require(archive.testzip() is None, 'Archive CRC validation failed')


def images(directory, ctx):
    value = load_json(regular(directory, 'images.json'))
    require(isinstance(value, dict), 'Invalid image digest document')
    names = ['candidate'] if ctx['mode'] == 'candidate' else ['application', 'worker', 'broker', 'postgres']
    require(set(value) == set(names), 'Unexpected image components')
    for component in names:
        expected = CANDIDATE_IMAGE if ctx['mode'] == 'candidate' else IMAGE_ROOT + '/' + component
        require(isinstance(value[component], dict) and set(value[component]) == {'name', 'digest'},
                'Invalid image digest record')
        require(value[component]['name'] == expected, 'Image repository is not trusted')
        require(DIGEST.fullmatch(value[component]['digest']) is not None, 'An immutable image digest is required')
    return value


def payload_names(directory, ctx):
    names = archive_names(ctx) + ['images.json']
    if ctx['mode'] == 'release':
        names += ['image-scan-summary.json']
    for name in names:
        path = regular(directory, name, MAX_ARCHIVE if name.endswith('.zip') else MAX_METADATA)
        if name.endswith('.zip'):
            check_zip(path)
    found = {p.name for p in directory.iterdir() if p.name.endswith('.zip')}
    require(found == set(archive_names(ctx)), 'Archive set does not match the expected release/candidate')
    return sorted(names)


def make_provenance(ctx, sums, image_records):
    subject = [{'name': name, 'digest': {'sha256': digest}} for name, digest in sorted(sums.items())]
    subject += [{'name': item['name'], 'digest': {'sha256': item['digest'][7:]}}
                for _, item in sorted(image_records.items())]
    return {'_type': 'https://in-toto.io/Statement/v1', 'subject': subject,
            'predicateType': PREDICATE, 'predicate': {
                'buildDefinition': {'buildType': BUILD_TYPE,
                    'externalParameters': ctx,
                    'resolvedDependencies': [{'uri': 'git+https://github.com/' + REPOSITORY,
                                              'digest': {'gitCommit': ctx['commit']}}]},
                'runDetails': {'builder': {'id': f'https://github.com/{REPOSITORY}/{WORKFLOW}@{ctx["ref"]}'},
                    'metadata': {'invocationId': f'https://github.com/{REPOSITORY}/actions/runs/{ctx["run_id"]}/attempts/{ctx["run_attempt"]}'}}}}


def prepare(directory, ctx):
    require(not (directory / 'SHA256SUMS').exists() and not (directory / 'provenance.json').exists(),
            'Refusing to replace existing release metadata')
    image_records = images(directory, ctx)
    sums = {name: sha256(regular(directory, name, MAX_ARCHIVE if name.endswith('.zip') else MAX_METADATA))
            for name in payload_names(directory, ctx)}
    (directory / 'SHA256SUMS').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(sums.items())))
    (directory / 'provenance.json').write_text(json.dumps(make_provenance(ctx, sums, image_records), indent=2) + '\n')


def verify_metadata(directory, ctx):
    expected = set(payload_names(directory, ctx))
    sums = {}
    for line in regular(directory, 'SHA256SUMS').read_text().splitlines():
        match = re.fullmatch(r'([a-f0-9]{64})  ([A-Za-z0-9][A-Za-z0-9._-]{0,180})', line)
        require(match is not None, 'Malformed checksum line')
        digest, name = match.groups()
        require(name in expected and name not in sums, 'Unexpected/duplicate checksum member')
        require(sha256(regular(directory, name, MAX_ARCHIVE if name.endswith('.zip') else MAX_METADATA)) == digest,
                'Artifact checksum mismatch')
        sums[name] = digest
    require(set(sums) == expected, 'Incomplete release checksums')
    image_records = images(directory, ctx)
    require(load_json(regular(directory, 'provenance.json')) == make_provenance(ctx, sums, image_records),
            'Provenance repository/workflow/ref/commit/run/subjects do not match the expected release')
    return image_records


def cosign_environment():
    # Cosign BindViper maps COSIGN_* into unspecified flags. Inherited local/CI
    # settings must not select a key, custom root or insecure verification mode.
    # Preserve ACTIONS_* for ambient GitHub OIDC and normal registry read auth.
    return {key: value for key, value in os.environ.items()
            if not key.startswith(('COSIGN_', 'SIGSTORE_'))}


def cosign_version(cosign, runner=run):
    result = runner([cosign, 'version', '--json'], capture_output=True, text=True, env=cosign_environment())
    require(json.loads(result.stdout).get('gitVersion') == COSIGN_VERSION, 'Install reviewed Cosign ' + COSIGN_VERSION)


def verify(directory, ctx, cosign='cosign', runner=run):
    cosign_version(cosign, runner)
    flags = certificate_flags(ctx)
    # Authenticate metadata before trusting ANY repository, digest or asset name.
    for blob in ['SHA256SUMS', 'provenance.json']:
        runner([cosign, 'verify-blob', '--bundle', str(regular(directory, blob + '.sigstore.json')),
                *flags, str(regular(directory, blob))], env=cosign_environment())
    image_records = verify_metadata(directory, ctx)
    expected_files=set(payload_names(directory,ctx))|{'SHA256SUMS','provenance.json','SHA256SUMS.sigstore.json','provenance.json.sigstore.json'}|{component+'.sigstore.json' for component in image_records}
    require({p.name for p in directory.iterdir()}==expected_files,'Unexpected or missing release files')
    for name in expected_files:regular(directory,name,MAX_ARCHIVE if name.endswith('.zip') else MAX_METADATA)
    for _, item in sorted(image_records.items()):
        command = [cosign, 'verify', *flags, '-a', 'git_sha=' + ctx['commit'],
                   '-a', 'git_ref=' + ctx['ref']]
        if ctx['mode'] == 'candidate':
            # This exception cannot be selected by a release artifact or tag mode.
            command += ['--allow-http-registry']
        runner(command + [item['name'] + '@' + item['digest']], env=cosign_environment())
    print('Verified exact ARTEX repository/workflow/ref/commit, artifact checksums, provenance and image signatures')


def sign(directory, ctx, cosign='cosign'):
    require(os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('GITHUB_REPOSITORY') == REPOSITORY
            and os.environ.get('GITHUB_REF') == ctx['ref'] and os.environ.get('GITHUB_SHA') == ctx['commit'],
            'Keyless signing is restricted to the expected GitHub Actions context')
    cosign_version(cosign)
    for component, item in sorted(images(directory, ctx).items()):
        command = [cosign, 'sign', '--yes', '--oidc-provider=github-actions',
                   '--bundle', str(directory / (component + '.sigstore.json')),
                   '-a', 'git_sha=' + ctx['commit'], '-a', 'git_ref=' + ctx['ref']]
        if ctx['mode'] == 'candidate':
            command += ['--allow-http-registry']
        run(command + [item['name'] + '@' + item['digest']], env=cosign_environment())
    prepare(directory, ctx)
    for blob in ['SHA256SUMS', 'provenance.json']:
        run([cosign, 'sign-blob', '--yes', '--oidc-provider=github-actions',
             '--bundle', str(directory / (blob + '.sigstore.json')), str(directory / blob)], env=cosign_environment())
    verify(directory, ctx, cosign)


def candidate(directory, ctx):
    directory.mkdir(parents=True, exist_ok=True)
    require(not any(directory.iterdir()), 'Candidate directory must be empty')
    with zipfile.ZipFile(directory / archive_names(ctx)[0], 'w', zipfile.ZIP_DEFLATED) as archive:
        archive.writestr('candidate.txt', 'Synthetic signing test, NOT a production build.\n' + json.dumps(ctx) + '\n')
    # A real minimal scratch image is built/pushed only into the disposable CI
    # loopback registry; no GitHub package/tag/release is created in this mode.
    build = directory.parent / 'candidate-image'
    build.mkdir(exist_ok=False)
    (build / 'candidate.txt').write_text('ARTEX keyless signature test\n')
    (build / 'Dockerfile').write_text('FROM scratch\nCOPY candidate.txt /candidate.txt\n')
    image = CANDIDATE_IMAGE + ':' + ctx['run_id'] + '-' + ctx['run_attempt']
    run(['docker', 'build', '--tag', image, str(build)])
    run(['docker', 'push', image])
    result = run(['docker', 'image', 'inspect', image, '--format', '{{json .RepoDigests}}'], capture_output=True, text=True)
    digests = json.loads(result.stdout)
    matches = [value.split('@', 1)[1] for value in digests if value.startswith(CANDIDATE_IMAGE + '@')]
    require(len(matches) == 1 and DIGEST.fullmatch(matches[0]), 'Missing candidate registry digest')
    (directory / 'images.json').write_text(json.dumps({'candidate': {'name': CANDIDATE_IMAGE, 'digest': matches[0]}}, indent=2) + '\n')


def gate(env=None, runner=run):
    env = os.environ if env is None else env
    require(env.get('GITHUB_REPOSITORY') == REPOSITORY, 'Release workflow is bound to ' + REPOSITORY)
    require(env.get('GITHUB_WORKFLOW_REF') == f'{REPOSITORY}/{WORKFLOW}@{env.get("GITHUB_REF")}',
            'Unexpected workflow identity')
    event, ref = env.get('GITHUB_EVENT_NAME'), env.get('GITHUB_REF', '')
    if event == 'workflow_dispatch':
        require(env.get('ARTEX_DRY_RUN') == 'true' and ref == 'refs/heads/main', 'Dispatch supports main-only dry-run')
        mode = 'candidate'
    else:
        require(event == 'push' and env.get('ARTEX_ENABLE_RELEASES') == 'true'
                and env.get('ARTEX_REF_PROTECTED') == 'true', 'Publication requires opt-in and protected version tag')
        mode = 'release'
    ctx = context(mode, ref, env.get('GITHUB_SHA', ''), env.get('GITHUB_RUN_ID', ''), env.get('GITHUB_RUN_ATTEMPT', ''))
    head = runner(['git', 'rev-parse', 'HEAD'], capture_output=True, text=True).stdout.strip()
    require(head == ctx['commit'], 'Checkout does not match the triggering commit')
    runner(['git', 'merge-base', '--is-ancestor', ctx['commit'], 'origin/main'])
    if mode == 'release':
        tagged = runner(['git', 'rev-parse', ref + '^{commit}'], capture_output=True, text=True).stdout.strip()
        require(tagged == ctx['commit'], 'Version tag does not match the reviewed checkout')
        remote=runner(['git','ls-remote','--exit-code','origin',ref,ref+'^{}'],capture_output=True,text=True).stdout.splitlines()
        references={line.split('\t',1)[1]:line.split('\t',1)[0] for line in remote if '\t' in line}
        require(references.get(ref+'^{}',references.get(ref))==ctx['commit'],'Remote version tag moved or disappeared')
    if env.get('GITHUB_OUTPUT'):
        with open(env['GITHUB_OUTPUT'], 'a') as output:
            output.write('mode=' + mode + '\n')
    print('Validated ' + mode + ' workflow context')
    return ctx


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['gate', 'candidate', 'prepare', 'sign', 'verify'])
    parser.add_argument('--directory', type=Path)
    selector = parser.add_mutually_exclusive_group()
    selector.add_argument('--tag')
    selector.add_argument('--candidate-run')
    parser.add_argument('--attempt', default='1')
    parser.add_argument('--commit')
    parser.add_argument('--run-id')
    parser.add_argument('--cosign', default='cosign')
    args = parser.parse_args()
    if args.operation == 'gate':
        gate()
        return
    require(args.directory is not None and args.commit is not None, 'Directory and expected commit are required')
    if args.tag:
        ctx = context('release', 'refs/tags/' + args.tag, args.commit, args.run_id or '', args.attempt)
    else:
        require(args.candidate_run is not None, 'Select an explicit tag or candidate run')
        ctx = context('candidate', 'refs/heads/main', args.commit, args.candidate_run, args.attempt)
    if args.operation == 'candidate':
        candidate(args.directory, ctx)
    elif args.operation == 'prepare':
        prepare(args.directory, ctx)
    elif args.operation == 'sign':
        sign(args.directory, ctx, args.cosign)
    else:
        verify(args.directory, ctx, args.cosign)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, zipfile.BadZipFile) as error:
        print('Signed release refused: ' + str(error), file=sys.stderr)
        sys.exit(1)

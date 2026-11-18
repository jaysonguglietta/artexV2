#!/usr/bin/env python3
"""Build/scan every release architecture before publishing version-only images."""
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('signed_release', ROOT / 'scripts/signed-release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
COMPONENTS = ('application', 'worker', 'broker', 'postgres')
PLATFORMS = ('amd64', 'arm64')
MANIFEST_TYPES = {'application/vnd.oci.image.manifest.v1+json', 'application/vnd.docker.distribution.manifest.v2+json'}
INDEX_TYPES = {'application/vnd.oci.image.index.v1+json', 'application/vnd.docker.distribution.manifest.list.v2+json'}
CONFIG_TYPES = {'application/vnd.oci.image.config.v1+json', 'application/vnd.docker.container.image.v1+json'}
LAYER_TYPES = {'application/vnd.oci.image.layer.v1.tar', 'application/vnd.oci.image.layer.v1.tar+gzip',
               'application/vnd.oci.image.layer.v1.tar+zstd', 'application/vnd.docker.image.rootfs.diff.tar.gzip'}
MAX_REGISTRY_JSON = 4 << 20


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError('Registry redirects are prohibited')


OPENER = urllib.request.build_opener(NoRedirect())


def command(args, **kwargs):
    return subprocess.run(args, check=True, timeout=3600, cwd=ROOT, **kwargs)


def registry_token(repository):
    credential = os.environ['GITHUB_ACTOR'] + ':' + os.environ['GH_TOKEN']
    request = urllib.request.Request('https://ghcr.io/token?' + urllib.parse.urlencode({
        'service': 'ghcr.io', 'scope': 'repository:' + repository + ':pull,push'}),
        headers={'Authorization': 'Basic ' + base64.b64encode(credential.encode()).decode()})
    with OPENER.open(request, timeout=20) as response:
        result = json.load(response)
    token = result.get('token')
    release.require(isinstance(token, str) and token, 'Registry credential exchange failed')
    return token


def manifest_digest(repository, tag, absent=False):
    # Registry bearer tokens expire independently of GITHUB_TOKEN. Fetch one
    # immediately for each HEAD; eight image uploads can outlive an earlier
    # existence-check token. Authentication failures still fail closed.
    token = registry_token(repository)
    request = urllib.request.Request('https://ghcr.io/v2/' + repository + '/manifests/' + tag,
        method='HEAD', headers={'Authorization': 'Bearer ' + token,
        'Accept': 'application/vnd.oci.image.index.v1+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json'})
    try:
        with OPENER.open(request, timeout=20) as response:
            digest = response.headers.get('Docker-Content-Digest', '')
    except urllib.error.HTTPError as error:
        code=error.code
        error.close()
        if code == 404 and absent:
            return None
        raise ValueError('Registry manifest check failed closed (HTTP ' + str(code) + ')') from None
    release.require(not absent, 'Refusing to replace an existing version image tag')
    release.require(release.DIGEST.fullmatch(digest), 'Registry omitted immutable digest')
    return digest


def scanned_identities(summary, arch):
    release.require(isinstance(summary, dict) and summary.get('status') == 'passed'
                    and isinstance(summary.get('images'), list), 'Missing successful image scan inventory')
    records = {}
    for image in summary['images']:
        release.require(isinstance(image, dict) and image.get('component') in COMPONENTS,
                        'Unknown scanned image component')
        component = image['component']
        release.require(component not in records and image.get('platform') == 'linux/' + arch
                        and image.get('blocking') == [], 'Ambiguous or blocked scanned image identity')
        digest, layers = image.get('config_digest'), image.get('rootfs_diff_ids')
        release.require(isinstance(digest, str) and release.DIGEST.fullmatch(digest)
                        and isinstance(layers, list) and 0 < len(layers) <= 256
                        and all(isinstance(value, str) and release.DIGEST.fullmatch(value) for value in layers),
                        'Missing authenticated scanned config/rootfs identity')
        records[component] = {'config_digest': digest, 'rootfs_diff_ids': layers,
                              'platform': image['platform']}
    release.require(set(records) == set(COMPONENTS), 'Incomplete scanned component inventory')
    return records


def registry_document(repository, digest):
    release.require(repository in {release.IMAGE_ROOT.removeprefix('ghcr.io/') + '/' + name for name in COMPONENTS},
                    'Untrusted registry repository')
    release.require(isinstance(digest, str) and release.DIGEST.fullmatch(digest), 'Invalid immutable registry digest')
    request = urllib.request.Request('https://ghcr.io/v2/' + repository + '/manifests/' + digest,
        headers={'Authorization': 'Bearer ' + registry_token(repository),
                 'Accept': ','.join(sorted(MANIFEST_TYPES | INDEX_TYPES))})
    with OPENER.open(request, timeout=20) as response:
        media = response.headers.get('Content-Type', '').split(';', 1)[0].strip()
        raw = response.read(MAX_REGISTRY_JSON + 1)
    release.require(0 < len(raw) <= MAX_REGISTRY_JSON, 'Registry document exceeds bounded JSON limit')
    release.require('sha256:' + hashlib.sha256(raw).hexdigest() == digest, 'Registry document digest mismatch')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            release.require(key not in result, 'Duplicate registry JSON key')
            result[key] = value
        return result
    value = json.loads(raw, object_pairs_hook=unique)
    release.require(isinstance(value, dict) and media in MANIFEST_TYPES | INDEX_TYPES
                    and value.get('mediaType') == media and type(value.get('schemaVersion')) is int
                    and value['schemaVersion'] == 2, 'Invalid registry document schema/media type')
    return value


def annotations(value):
    release.require(isinstance(value, dict) and all(isinstance(k, str) and isinstance(v, str)
                    for k, v in value.items()), 'Invalid registry annotations')


def descriptor(value, allowed_types, platform=False):
    allowed = {'mediaType', 'digest', 'size', 'annotations'} | ({'platform'} if platform else set())
    release.require(isinstance(value, dict) and set(value) <= allowed
                    and {'mediaType', 'digest', 'size'} <= set(value),
                    'Invalid descriptor fields; foreign URLs/inline data are prohibited')
    release.require(isinstance(value['mediaType'], str) and value['mediaType'] in allowed_types and isinstance(value['digest'], str)
                    and release.DIGEST.fullmatch(value['digest']) and type(value['size']) is int
                    and 0 < value['size'] <= (16 << 30), 'Invalid descriptor digest/size/media type')
    if 'annotations' in value:
        annotations(value['annotations'])
    if platform:
        target = value.get('platform')
        release.require(isinstance(target, dict) and set(target) <= {'os', 'architecture', 'variant'}
                        and target.get('os') == 'linux' and target.get('architecture') in PLATFORMS,
                        'Invalid registry child platform')
        release.require(isinstance(target.get('variant', ''), str)
                        and target.get('variant', '') in ({'', 'v8'} if target['architecture'] == 'arm64' else {''}),
                        'Unexpected registry architecture variant')
        return target['architecture']


def index_children(value, expected):
    release.require(isinstance(value, dict) and isinstance(value.get('mediaType'), str) and value.get('mediaType') in INDEX_TYPES
                    and set(value) <= {'schemaVersion', 'mediaType', 'manifests', 'annotations'}
                    and isinstance(value.get('manifests'), list)
                    and len(value['manifests']) == len(expected), 'Invalid registry index child inventory')
    if 'annotations' in value:
        annotations(value['annotations'])
    found = {}
    for child in value['manifests']:
        arch = descriptor(child, MANIFEST_TYPES, platform=True)
        release.require(arch not in found and arch in expected and child['digest'] == expected[arch],
                        'Registry index child/platform substitution')
        found[arch] = child
    release.require(set(found) == set(expected), 'Incomplete registry index architecture set')
    return found


def verify_pushed_image(repository, digest, identity):
    source_digest = digest
    value = registry_document(repository, digest)
    arch = identity['platform'].removeprefix('linux/')
    # Docker 29 can push a one-platform OCI index rather than a leaf manifest.
    # Normalize it to its sole authenticated child before assembling our index.
    if value['mediaType'] in INDEX_TYPES:
        children = value.get('manifests')
        release.require(isinstance(children, list) and len(children) == 1
                        and isinstance(children[0], dict), 'Pushed image index must contain exactly one platform')
        child = index_children(value, {arch: children[0].get('digest')})[arch]
        digest = child['digest']
        value = registry_document(repository, digest)
    release.require(value.get('mediaType') in MANIFEST_TYPES
                    and set(value) <= {'schemaVersion', 'mediaType', 'config', 'layers', 'annotations'}
                    and isinstance(value.get('layers'), list), 'Invalid pushed image manifest')
    if 'annotations' in value:
        annotations(value['annotations'])
    config = value.get('config')
    descriptor(config, CONFIG_TYPES)
    release.require(config['size'] <= MAX_REGISTRY_JSON and config['digest'] == identity['config_digest'],
                    'Pushed manifest config differs from the scanned config')
    release.require(len(value['layers']) == len(identity['rootfs_diff_ids']),
                    'Pushed manifest rootfs layer inventory differs from the scanned config')
    for layer in value['layers']:
        descriptor(layer, LAYER_TYPES)
    # The scanned config SHA commits to exact OS/arch and rootfs diffIDs. An
    # OCI runtime also verifies layer contents against those diffIDs on unpack.
    # No CDN redirects or untrusted remote config bodies are needed here.
    return {**identity, 'digest': digest, 'push_digest': source_digest,
            'manifest_config': config, 'manifest_layers': value['layers']}


def pushed_digest(result, repository, tag):
    matches = re.findall(r'(?m)^' + re.escape(tag) + r': digest: (sha256:[a-f0-9]{64}) size: [0-9]+\s*$', result.stdout or '')
    release.require(len(matches) <= 1, 'Ambiguous Docker push digest output')
    # Older/newer Docker output can vary. A tag lookup is allowed only because
    # verify_pushed_image then binds its immutable content to the scan identity.
    return matches[0] if matches else manifest_digest(repository, tag)


def build_scan_publish(output):
    ctx = release.gate()
    release.require(ctx['mode'] == 'release', 'Image publication requires protected release context')
    output.mkdir(parents=True, exist_ok=True)
    release.require(not any(output.iterdir()), 'Release output directory must be empty')
    # The reviewed PostgreSQL target retains the pinned upstream database engine
    # and replaces its obsolete privilege helper; scan/sign this exact derivative.
    scans, identities = {}, {}
    for arch in PLATFORMS:
        for component in COMPONENTS:
            command(['docker', 'buildx', 'build', '--load', '--platform', 'linux/' + arch,
                     '--provenance=false', '--sbom=false',
                     '--build-arg','ARTEX_BUILD_VERSION='+ctx['ref'][11:],
                     '--build-arg','ARTEX_BUILD_COMMIT='+ctx['commit'], '-f', 'Dockerfile.source', '--target', component,
                     '--label', 'org.opencontainers.image.revision=' + ctx['commit'],
                     '-t', f'artex-release-{component}-{arch}', '.'])
        scan_dir = output.parent / ('image-scans-' + arch)
        command([sys.executable, 'scripts/scan-images.py', '--output', str(scan_dir),
                 '--platform', 'linux/' + arch,
                 *[argument for component in COMPONENTS
                   for argument in ('--image', f'{component}=artex-release-{component}-{arch}')]])
        scans[arch] = json.loads((scan_dir / 'summary.json').read_text())
        identities[arch] = scanned_identities(scans[arch], arch)
    # Only after every architecture/component passed the fail-closed scanner may
    # this job perform registry version-existence checks or push any image.
    tag = ctx['ref'][10:]
    for component in COMPONENTS:
        repository = release.IMAGE_ROOT.removeprefix('ghcr.io/') + '/' + component
        for candidate_tag in [tag, *[tag + '-' + arch for arch in PLATFORMS]]:
            manifest_digest(repository, candidate_tag, absent=True)
    images, published, bindings = {}, {}, {}
    for component in COMPONENTS:
        image = release.IMAGE_ROOT + '/' + component
        repository = image.removeprefix('ghcr.io/')
        children = []
        published[component] = {}
        bindings[component] = {}
        for arch in PLATFORMS:
            reference = image + ':' + tag + '-' + arch
            command(['docker', 'tag', f'artex-release-{component}-{arch}', reference])
            result = command(['docker', 'push', reference], capture_output=True, text=True)
            digest = pushed_digest(result, repository, tag + '-' + arch)
            binding = verify_pushed_image(repository, digest, identities[arch][component])
            digest = binding['digest']
            children.append(image + '@' + digest)
            published[component][arch] = digest
            bindings[component][arch] = binding
        command(['docker', 'buildx', 'imagetools', 'create', '--tag', image + ':' + tag, *children])
        index_digest = manifest_digest(repository, tag)
        index_children(registry_document(repository, index_digest), published[component])
        images[component] = {'name': image,
                             'digest': index_digest}
    (output / 'images.json').write_text(json.dumps(images, indent=2) + '\n')
    (output / 'image-scan-summary.json').write_text(json.dumps({
        'commit': ctx['commit'], 'platform_scans': scans, 'published_architecture_digests': published,
        'published_scan_identity_bindings': bindings}, indent=2) + '\n')


if __name__ == '__main__':
    try:
        release.require(len(sys.argv) == 2, 'Usage: release-images.py OUTPUT_DIRECTORY')
        build_scan_publish(Path(sys.argv[1]).resolve())
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print('Release images refused: ' + str(error), file=sys.stderr)
        sys.exit(1)

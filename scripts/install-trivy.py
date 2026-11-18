#!/usr/bin/env python3
"""Install the checksum-pinned official Trivy release without extracting its tree.

Existing executables must match the pinned binary digest, or an executable freshly
read from the checksum-verified release archive. Local receipt files are not trust
anchors. No shell, package-manager installation, or downloaded script is executed.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import stat
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
MAX_DOWNLOAD = 200 * 1024 * 1024
MAX_BINARY = 400 * 1024 * 1024
MAX_UNPACKED = 512 * 1024 * 1024


def sha256_file(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def host_platform():
    machine = platform.machine().lower()
    architecture = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'amd64': 'amd64'}.get(machine)
    return platform.system() + '/' + str(architecture)


def release_pin():
    pins = json.loads((ROOT / 'security/tool-pins.json').read_text())['trivy']
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', pins.get('version', '')) or pins.get('release_base') != 'https://github.com/aquasecurity/trivy/releases/download/v' + pins['version'] + '/':
        raise ValueError('Trivy pins must name an exact official versioned release')
    key = host_platform()
    if key not in pins['assets']:
        raise ValueError('Unsupported scanner host platform: ' + key)
    asset = pins['assets'][key]
    if not isinstance(asset.get('name'), str) or not re.fullmatch(r'trivy_[0-9]+\.[0-9]+\.[0-9]+_(?:macOS-ARM64|Linux-64bit|Linux-ARM64)\.tar\.gz', asset['name']) or not asset['name'].startswith('trivy_' + pins['version'] + '_'):
        raise ValueError('Unexpected Trivy release asset name')
    if not re.fullmatch('[0-9a-f]{64}', asset['sha256']):
        raise ValueError('Malformed pinned Trivy archive digest')
    return pins, asset


class ReleaseRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        parsed = urllib.parse.urlsplit(new_url)
        if parsed.scheme != 'https' or parsed.hostname not in {
            'github.com', 'release-assets.githubusercontent.com', 'objects.githubusercontent.com',
        } or parsed.username or parsed.password:
            raise ValueError('Refusing unexpected Trivy release redirect')
        return super().redirect_request(request, response, code, message, headers, new_url)


def download_archive(url, destination):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != 'https' or parsed.hostname != 'github.com':
        raise ValueError('Trivy archive must come from the pinned official HTTPS release')
    opener = urllib.request.build_opener(ReleaseRedirects())
    with opener.open(urllib.request.Request(url, headers={'User-Agent': 'ARTEX-image-security/1'}), timeout=60) as response:
        length = response.headers.get('Content-Length')
        if length is not None and (not length.isdecimal() or int(length) > MAX_DOWNLOAD):
            raise ValueError('Trivy download exceeds the bounded archive size')
        count = 0
        started = time.monotonic()
        with Path(destination).open('xb') as output:
            while chunk := response.read(1024 * 1024):
                count += len(chunk)
                if time.monotonic() - started > 300:
                    raise ValueError('Trivy release download exceeded its time budget')
                if count > MAX_DOWNLOAD:
                    raise ValueError('Trivy download exceeds the bounded archive size')
                output.write(chunk)


def binary_from_archive(archive, expected_digest):
    if sha256_file(archive) != expected_digest:
        raise ValueError('Trivy archive checksum mismatch')
    executable = None
    total = 0
    with tarfile.open(archive, mode='r:gz') as bundle:
        for count, member in enumerate(bundle, 1):
            total += max(member.size, 0)
            if count > 256 or total > MAX_UNPACKED:
                raise ValueError('Trivy archive inventory exceeds extraction limits')
            if member.name not in ('trivy', './trivy'):
                continue
            if executable is not None or not member.isreg() or not 0 < member.size <= MAX_BINARY:
                raise ValueError('Trivy archive must contain one bounded regular executable')
            with bundle.extractfile(member) as source:
                executable = source.read(MAX_BINARY + 1)
            if len(executable) != member.size:
                raise ValueError('Incomplete Trivy executable in archive')
    if executable is None:
        raise ValueError('Trivy executable missing from archive')
    return executable


def existing_digest(destination):
    metadata = destination.lstat()
    if not stat.S_ISREG(metadata.st_mode) or not metadata.st_mode & stat.S_IXUSR:
        raise ValueError('Existing Trivy path must be a regular executable, never a symlink')
    descriptor = os.open(destination, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as source:
        actual = os.fstat(source.fileno())
        if (actual.st_dev, actual.st_ino) != (metadata.st_dev, metadata.st_ino):
            raise ValueError('Trivy executable changed while opening')
        return hashlib.file_digest(source, 'sha256').hexdigest()


def install(destination):
    destination = Path(destination).expanduser().absolute()
    pins, asset = release_pin()
    digest = None
    if os.path.lexists(destination):
        digest = existing_digest(destination)
        if asset.get('binary_sha256'):
            if digest != asset['binary_sha256']:
                raise ValueError('Existing Trivy executable checksum mismatch')
            return destination, dict(asset, version=pins['version'], verified_binary_sha256=digest)
    with tempfile.TemporaryDirectory(prefix='artex-trivy-release-') as temporary:
        archive = Path(temporary) / 'release.tar.gz'
        download_archive(pins['release_base'] + asset['name'], archive)
        data = binary_from_archive(archive, asset['sha256'])
        expected = hashlib.sha256(data).hexdigest()
        if asset.get('binary_sha256') and expected != asset['binary_sha256']:
            raise ValueError('Pinned archive and executable digests disagree')
        if digest is not None:
            if digest != expected:
                raise ValueError('Existing Trivy executable checksum mismatch')
        else:
            destination.parent.mkdir(parents=True, exist_ok=True)
            descriptor = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o700)
            with os.fdopen(descriptor, 'wb') as output:
                output.write(data)
    return destination, dict(asset, version=pins['version'], verified_binary_sha256=expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=Path.home() / '.cache/artex-security-tools/trivy-0.76.2')
    args = parser.parse_args()
    try:
        executable, provenance = install(args.output)
    except (OSError, ValueError, tarfile.TarError) as error:
        parser.exit(1, f'Trivy installation refused: {error}\n')
    print(json.dumps({'executable': str(executable), **provenance}, indent=2))


if __name__ == '__main__':
    main()

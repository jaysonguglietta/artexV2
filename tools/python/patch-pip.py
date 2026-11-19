#!/usr/bin/env python3
"""Build an honest Python3.14-only pip wheel without its unused legacy backend.

The pinned upstream pip26.3 explicitly cannot select pkg_resources on Python3.14.
Remove that complete vendored component and its backend, rather than changing
vulnerable component versions or hiding their scanner metadata.
"""
import argparse
import base64
import csv
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import stat
import zipfile

UPSTREAM_SHA256 = 'f3b60b20ffaadd4415ed7957212a38ac63fa4c5fa375bd0f4a797777940fad13'
VERSION = '26.3+artex.1'
OLD_INFO = 'pip-26.3.dist-info/'
NEW_INFO = 'pip-' + VERSION + '.dist-info/'


def patch_wheel(source, destination):
    raw = source.read_bytes()
    if hashlib.sha256(raw).hexdigest() != UPSTREAM_SHA256:
        raise ValueError('Upstream pip wheel checksum mismatch')
    files = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        total = 0
        for entry in archive.infolist():
            name = entry.filename
            parts = PurePosixPath(name).parts
            total += entry.file_size
            if (name in files or '\\' in name or ':' in name or not parts
                    or name.startswith('/') or any(p in ('', '.', '..') for p in name.split('/'))
                    or stat.S_IFMT(entry.external_attr >> 16) not in (0, stat.S_IFREG)
                    or entry.file_size > 8 << 20 or total > 32 << 20):
                raise ValueError('Unsafe or oversized pip wheel')
            files[name] = archive.read(entry)
    backend = files['pip/_internal/metadata/__init__.py']
    if b'if sys.version_info >= (3, 14):' not in backend or b'only support importlib.metadata' not in backend:
        raise ValueError('Upstream no longer proves the backend unused on Python3.14')
    removed = [name for name in files if name.startswith('pip/_vendor/pkg_resources/')
               or name == 'pip/_internal/metadata/pkg_resources.py'
               or name.startswith(OLD_INFO + 'licenses/src/pip/_vendor/pkg_resources/')]
    if not removed or 'pip/_vendor/pkg_resources/__init__.py' not in removed:
        raise ValueError('Unexpected upstream component inventory')
    for name in removed:
        del files[name]
    vendor = files['pip/_vendor/vendor.txt']
    if vendor.count(b'setuptools==70.3.0\n') != 1:
        raise ValueError('Unexpected setuptools vendor declaration')
    files['pip/_vendor/vendor.txt'] = vendor.replace(b'setuptools==70.3.0\n', b'')
    # Correct the retained upstream SBOM only after removing the actual code.
    # All other components and their dependency relationships remain present.
    bom_name = 'pip/_vendor/bom.cdx.json'
    bom = json.loads(files[bom_name])
    removed_refs = {component['bom-ref'] for component in bom['components']
                    if component.get('name') == 'setuptools' and component.get('version') == '70.3.0'}
    if bom.get('bomFormat') != 'CycloneDX' or removed_refs != {'pkg:pypi/setuptools@70.3.0'}:
        raise ValueError('Unexpected upstream vendored SBOM identity')
    bom['components'] = [component for component in bom['components'] if component['bom-ref'] not in removed_refs]
    dependencies = []
    for dependency in bom['dependencies']:
        if dependency['ref'] in removed_refs:
            continue
        if 'dependsOn' in dependency:
            dependency['dependsOn'] = [ref for ref in dependency['dependsOn'] if ref not in removed_refs]
        dependencies.append(dependency)
    bom['dependencies'] = dependencies
    bom['metadata']['component']['version'] = VERSION
    bom['version'] += 1
    files[bom_name] = (json.dumps(bom, indent=2) + '\n').encode()
    init = files['pip/__init__.py']
    if init.count(b'__version__ = "26.3"') != 1:
        raise ValueError('Unexpected upstream pip identity')
    files['pip/__init__.py'] = init.replace(b'__version__ = "26.3"', ('__version__ = "' + VERSION + '"').encode())
    metadata = files[OLD_INFO + 'METADATA'].decode()
    if metadata.count('Version: 26.3\n') != 1:
        raise ValueError('Unexpected pip distribution metadata')
    metadata = metadata.replace('Version: 26.3\n', 'Version: ' + VERSION + '\n')
    license_line = 'License-File: src/pip/_vendor/pkg_resources/LICENSE\n'
    if metadata.count(license_line) != 1:
        raise ValueError('Unexpected removed-component license declaration')
    metadata = metadata.replace(license_line, '')
    metadata, count = re.subn(r'^Requires-Python: .+$', 'Requires-Python: >=3.14', metadata, flags=re.M)
    if count != 1:
        raise ValueError('Missing pip interpreter requirement')
    files[OLD_INFO + 'METADATA'] = metadata.encode()
    files = {(NEW_INFO + k[len(OLD_INFO):] if k.startswith(OLD_INFO) else k): v
             for k, v in files.items() if k != OLD_INFO + 'RECORD'}
    record = io.StringIO(newline='')
    writer = csv.writer(record, lineterminator='\n')
    for name, data in sorted(files.items()):
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('=')
        writer.writerow([name, 'sha256=' + digest, len(data)])
    writer.writerow([NEW_INFO + 'RECORD', '', ''])
    files[NEW_INFO + 'RECORD'] = record.getvalue().encode()
    with destination.open('xb') as output, zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted(files.items()):
            entry = zipfile.ZipInfo(name, date_time=(2026, 11, 18, 0, 0, 0))
            entry.external_attr = (stat.S_IFREG | 0o644) << 16
            entry.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(entry, data)
    return len(removed)


def update_ensurepip(directory, wheel):
    init = directory / '__init__.py'
    data = init.read_text()
    if data.count('_PIP_VERSION = "26.2.2"') != 1:
        raise ValueError('Unexpected pinned Python ensurepip version')
    bundled = directory / '_bundled'
    old = list(bundled.glob('pip-*.whl'))
    if len(old) != 1 or old[0].name != 'pip-26.2.2-py3-none-any.whl':
        raise ValueError('Unexpected ensurepip wheel inventory')
    old[0].unlink()
    (bundled / wheel.name).write_bytes(wheel.read_bytes())
    init.write_text(data.replace('_PIP_VERSION = "26.2.2"', '_PIP_VERSION = "' + VERSION + '"'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path)
    parser.add_argument('destination', type=Path)
    parser.add_argument('--ensurepip', type=Path)
    args = parser.parse_args()
    count = patch_wheel(args.source, args.destination)
    if args.ensurepip:
        update_ensurepip(args.ensurepip, args.destination)
    print('Built pip ' + VERSION + '; removed ' + str(count) + ' unused legacy backend files')

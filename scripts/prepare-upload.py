#!/usr/bin/env python3
"""Create a reviewable source-only copy and deterministic ZIP, without publishing.

Skip runtime state, credentials and generated files. Refuse symlinks, special files
and recognized credential signatures. Read source files through no-follow directory
handles and snapshot their bytes before writing. This is a local preparation tool,
not a complete secret scanner; review its output before uploading anything.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import zipfile

SKIP_PARTS = {
    'node_modules', '.git', '.next', '__pycache__', '.secrets', '.codex', '.agents',
    '.claude', '.playwright-mcp', '.playwright-cli', '.idea', '.vscode', '.cache',
    '.swc-cache', '.turbo', '.pytest_cache', '.mypy_cache', '.ruff_cache', '.venv',
    '.aws', '.ssh', '.gnupg', '.docker', '.kube', '.azure', '.terraform',
}
SKIP_ROOTS = {
    'data', 'private', 'work', 'dist', 'bench', 'test-results', 'evidence-local',
    'scan-output', 'transcripts',
}
BINARY_NAMES = {'artex', 'tool-worker', 'worker-broker'}
SKIP_FILES = {
    'config.json', 'worker-policy.json', 'scheduler_state.json', 'jwt.key',
    '.DS_Store', 'Thumbs.db', 'next-env.d.ts', 'SOURCE-MANIFEST.json',
    '.npmrc', '.pypirc', '.netrc', '.git-credentials', '.gitconfig', '.bash_history',
    '.zsh_history', '.python_history', 'credentials', 'credentials.json',
    'application_default_credentials.json', 'service-account.json',
    'id_rsa', 'id_dsa', 'id_ecdsa', 'id_ed25519',
}
SKIP_SUFFIXES = (
    '.log', '.bak', '.key', '.pem', '.p12', '.pfx', '.db', '.db-wal', '.db-shm',
    '.sqlite', '.sqlite3', '.pyc', '.pyo', '.tsbuildinfo', '.local', '.exe', '.dll',
    '.dylib', '.so', '.o', '.a',
)
SECRET_PATTERNS = [
    # Includes PKCS#8 encrypted keys, DSA/OpenSSH keys and PGP private-key blocks.
    re.compile(rb'-{4,5}\s*BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?\s*-{4,5}'),
    re.compile(rb'gh[pousr]_[A-Za-z0-9]{30,}'),
    re.compile(rb'github_pat_[A-Za-z0-9_]{70,}'),
]
MACHO_MAGIC = {
    b'\xfe\xed\xfa\xce', b'\xce\xfa\xed\xfe', b'\xfe\xed\xfa\xcf',
    b'\xcf\xfa\xed\xfe', b'\xca\xfe\xba\xbe', b'\xbe\xba\xfe\xca',
    b'\xca\xfe\xba\xbf', b'\xbf\xba\xfe\xca',
}
ZIP_TIMESTAMP = (2026, 11, 18, 0, 0, 0)


def excluded(relative, *, is_directory=False):
    """Apply source-publication exclusions before inspecting or opening a path."""
    parts = relative.parts
    name = relative.name
    binary_artifact = any(
        name == binary or name == binary + '.exe'
        or any(name.startswith(binary + '.' + stage) for stage in ('new', 'old', 'failed', 'upgrade'))
        for binary in BINARY_NAMES
    )
    return (
        bool(SKIP_PARTS.intersection(parts)) or parts[0] in SKIP_ROOTS
        or any(p.startswith(('.scratch.', '.worker-fixture.', '.browser-test-deps.')) for p in parts)
        or parts[:2] == ('.config', 'gcloud')
        or parts[:2] == ('web', 'out') or parts[:3] == ('server', 'webui', 'dist')
        or name in SKIP_FILES or name.endswith(SKIP_SUFFIXES)
        or (binary_artifact and not is_directory)
        or (name.startswith('.env') and name != '.env.example')
    )


def compiled_binary(data):
    """Recognize native binaries even if renamed to bypass filename exclusions."""
    if data.startswith(b'\x7fELF') or data[:4] in MACHO_MAGIC:
        return True
    if len(data) >= 64 and data.startswith(b'MZ'):
        offset = int.from_bytes(data[60:64], 'little')
        return data[offset:offset + 4] == b'PE\0\0'
    return False


def read_source(root):
    """Snapshot regular source files without following source-controlled symlinks.

    fwalk verifies directory identity with follow_symlinks=False. Relative opens
    keep file access attached to the verified directory even if a path is renamed.
    O_NOFOLLOW rejects a file replaced by a symlink between stat and open, while
    O_NONBLOCK prevents a replacement FIFO from blocking before fstat rejects it.
    """
    if not hasattr(os, 'fwalk') or not hasattr(os, 'O_NOFOLLOW'):
        raise SystemExit('Secure preparation requires POSIX fwalk and O_NOFOLLOW support.')
    def fail_walk(error):
        raise SystemExit(f'Could not read source directory: {error.filename}') from None

    source_files = []
    for directory, dirs, names, directory_fd in os.fwalk(root, follow_symlinks=False, onerror=fail_walk):
        parent = Path(directory).relative_to(root)
        for name in sorted(dirs):
            relative = parent / name
            if excluded(relative, is_directory=True):
                dirs.remove(name)
                continue
            metadata = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
            if not stat.S_ISDIR(metadata.st_mode):
                raise SystemExit(f'Non-directory or symlink needs review: {relative}')
        dirs.sort()
        for name in sorted(names):
            relative = parent / name
            if excluded(relative):
                continue
            metadata = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
            if not stat.S_ISREG(metadata.st_mode):
                raise SystemExit(f'Nonregular file or symlink needs review: {relative}')
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            try:
                descriptor = os.open(name, flags, dir_fd=directory_fd)
            except OSError:
                raise SystemExit(f'Source changed or could not be safely opened: {relative}') from None
            with os.fdopen(descriptor, 'rb') as source:
                before = os.fstat(source.fileno())
                if not stat.S_ISREG(before.st_mode):
                    raise SystemExit(f'Nonregular file needs review: {relative}')
                if (before.st_dev, before.st_ino) != (metadata.st_dev, metadata.st_ino):
                    raise SystemExit(f'Source changed while opening: {relative}')
                data = source.read()
                after = os.fstat(source.fileno())
            identity = lambda item: (item.st_size, item.st_mtime_ns, item.st_ctime_ns)
            if identity(before) != identity(after) or len(data) != after.st_size:
                raise SystemExit(f'Source changed while reading: {relative}')
            if any(pattern.search(data) for pattern in SECRET_PATTERNS):
                raise SystemExit(f'Possible credential material needs review: {relative} (value redacted)')
            if compiled_binary(data):
                continue
            mode = 0o755 if before.st_mode & stat.S_IXUSR else 0o644
            source_files.append((relative, data, mode))
    return sorted(source_files, key=lambda entry: str(entry[0]))


def exists_including_symlink(path):
    return os.path.lexists(path)


def prepare(root, destination):
    """Prepare a new local review tree, ZIP and checksum; never overwrite a path."""
    root = root.resolve()
    if destination.is_symlink():
        raise SystemExit('Output must not be a symlink.')
    destination = destination.resolve()
    archive = destination.with_suffix('.zip')
    checksum_path = archive.with_suffix('.zip.sha256')
    if destination == root or root in destination.parents:
        raise SystemExit('Output must be a new directory outside the source tree.')
    if len({destination, archive, checksum_path}) != 3:
        raise SystemExit('Output directory, ZIP and checksum paths must be distinct.')
    if any(exists_including_symlink(path) for path in (destination, archive, checksum_path)):
        raise SystemExit('Output directory, ZIP or checksum already exists; refusing to overwrite it.')
    source_files = read_source(root)
    manifest = [
        {'path': str(relative), 'bytes': len(data), 'mode': oct(mode),
         'sha256': hashlib.sha256(data).hexdigest()}
        for relative, data, mode in source_files
    ]
    manifest_data = (json.dumps({
        'files': manifest,
        'excludes': 'Runtime state, credentials/configuration, generated/compiled files, dependency trees, VCS history, and local audit logs',
    }, indent=2) + '\n').encode()
    destination.mkdir(parents=True, exist_ok=False, mode=0o700)
    output_files = source_files + [(Path('SOURCE-MANIFEST.json'), manifest_data, 0o644)]
    for relative, data, mode in output_files:
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open('xb') as output:
            output.write(data)
        target.chmod(mode)
    # Exclusive creation closes the preflight-to-write race without replacing data.
    with archive.open('xb') as archive_output:
        with zipfile.ZipFile(archive_output, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zipped:
            for relative, data, mode in sorted(output_files, key=lambda entry: str(entry[0])):
                item = zipfile.ZipInfo(destination.name + '/' + str(relative), date_time=ZIP_TIMESTAMP)
                item.external_attr = (stat.S_IFREG | mode) << 16
                item.compress_type = zipfile.ZIP_DEFLATED
                zipped.writestr(item, data, compresslevel=9)
    checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
    with checksum_path.open('x') as output:
        output.write(checksum + '  ' + archive.name + '\n')
    print(f'Prepared {len(manifest)} source files: {destination}')
    print(f'ZIP: {archive}\nSHA256: {checksum}')
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    prepare(Path(__file__).resolve().parent.parent, args.output)


if __name__ == '__main__':
    main()

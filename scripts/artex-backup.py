#!/usr/bin/env python3
"""Create signed/encrypted ARTEX backups and verify them in a disposable database.

No production restore operation exists. OpenSSL3+ with CMS AES-GCM/RSA-PSS is
required. All plaintext staging is private and deleted after the operation.
"""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import stat
import struct
import subprocess
import tarfile
import tempfile
import threading
import time

from pgdisk import runtime_arguments
from backup_crypto import validate_cms_chunk

SOURCE = Path(__file__).resolve().parents[1]
DEFAULT_MAX_BYTES = 32 << 30
MAX_FILES = 400000
MAX_MANIFEST = 128 << 20
CONFIG_FILES = ['.env', '.env.workers', '.secrets/worker-token', '.secrets/worker-policy.json',
                'docker-compose.yml', 'compose.workers.yml']


def execute(arguments, *, input=None, stdout=subprocess.PIPE, timeout=120):
    result = subprocess.run(arguments, input=input, stdout=stdout, stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        # Docker/database/OpenSSL diagnostics may contain credentials or evidence.
        raise RuntimeError(arguments[0] + ' operation failed; no sensitive diagnostic output was printed')
    return result.stdout


def docker(arguments, **options):
    return execute(['docker', *arguments], **options)


def stream_command(arguments, destination, maximum, *, timeout=1800, input_bytes=None):
    process = subprocess.Popen(arguments, stdin=subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    expired = []
    def stop():
        expired.append(True)
        process.kill()
    timer = threading.Timer(timeout, stop)
    timer.start()
    try:
        if input_bytes is not None:
            process.stdin.write(input_bytes)
            process.stdin.close()
        copied = copy_bounded(process.stdout, destination, maximum)
        if process.wait(timeout=30) or expired: raise RuntimeError('Bounded database export failed or timed out')
        return copied
    finally:
        timer.cancel()
        if process.poll() is None: process.kill()
        process.wait()
        process.stdout.close()


def private_file(path, *, secret=False):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    if not stat.S_ISREG(info.st_mode) or info.st_size > 1 << 20:
        raise ValueError('Identity/certificate must be a bounded ordinary file')
    if secret and os.name == 'nt':
        raise ValueError('Run backup identity operations under Linux/macOS or a private WSL filesystem with POSIX permissions')
    if secret and os.name != 'nt' and info.st_mode & 0o077:
        raise ValueError('Private identity must have mode0600 or stricter')
    if secret and path.is_relative_to(SOURCE):
        raise ValueError('Private backup identities must be stored outside the source checkout')
    return path


def certificate_pin(path):
    raw = Path(path).read_bytes()
    if len(raw) > 1 << 20 or re.fullmatch(rb'\s*-----BEGIN CERTIFICATE-----\s*[A-Za-z0-9+/=\r\n]+?\s*-----END CERTIFICATE-----\s*', raw) is None:
        raise ValueError('Use one exact PEM certificate, not a trust bundle or combined identity')
    der = execute(['openssl', 'x509', '-in', str(path), '-outform', 'DER'])
    return hashlib.sha256(der).hexdigest()


def copy_bounded(source, target, limit):
    copied = 0
    while True:
        data = source.read(min(1 << 20, limit - copied + 1))
        if not data:
            return copied
        if len(data) > limit - copied:
            raise ValueError('Backup exceeds its reviewed byte limit')
        target.write(data)
        copied += len(data)


def digest_file(path):
    digest = hashlib.sha256()
    count = 0
    with path.open('rb') as handle:
        while data := handle.read(1 << 20):
            digest.update(data)
            count += len(data)
    return {'bytes': count, 'sha256': digest.hexdigest()}


def safe_member(name):
    path = PurePosixPath(name)
    if (not name or len(name.encode()) > 4096 or path.is_absolute() or '\\' in name or ':' in name
            or any(part in ('.', '..') for part in name.split('/')) or str(path) != name.rstrip('/')):
        raise ValueError('Unsafe backup archive member')
    return path


def extract_tar(source, directory, maximum):
    """Reject links/devices/duplicates before writing into a fresh private root."""
    paths = set()
    total = 0
    with tarfile.open(fileobj=source, mode='r|') as archive:
        for member in archive:
            name = member.name.rstrip('/')
            relative = safe_member(name)
            if name in paths or len(paths) >= MAX_FILES:
                raise ValueError('Duplicate or excessive backup archive members')
            paths.add(name)
            if not member.isdir() and not member.isfile():
                raise ValueError('Backup may contain only ordinary files and directories')
            target = directory.joinpath(*relative.parts)
            target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            if member.isdir():
                target.mkdir(exist_ok=True, mode=0o700)
                continue
            if member.size < 0 or member.size > maximum - total:
                raise ValueError('Backup archive exceeds its reviewed byte limit')
            with archive.extractfile(member) as incoming, target.open('xb') as outgoing:
                os.chmod(target, 0o600)
                copied = copy_bounded(incoming, outgoing, member.size)
                if copied != member.size:
                    raise ValueError('Short backup archive member')
            total += member.size
    return total


ENVELOPE = b'ARTEX-BACKUP-V1\n'
CHUNKED = b'ARTEX-CMS-CHUNKS-V1\n'
CHUNK_BYTES = 8 << 20
CMS_OVERHEAD = 1 << 20


class JoinedReader:
    def __init__(self, *sources): self.sources = list(sources)
    def read(self, count):
        result = bytearray()
        while self.sources and len(result) < count:
            data = self.sources[0].read(count - len(result))
            if data: result.extend(data)
            else: self.sources.pop(0)
        return bytes(result)


def write_encrypted_chunks(incoming, output, recipient, maximum):
    # CMS decoding buffers encrypted OCTET STRINGs; independent bounded standard
    # CMS messages keep OpenSSL heap finite even for a multi-gigabyte backup.
    command = ['openssl', 'cms', '-encrypt', '-binary', '-outform', 'DER', '-aes-256-gcm',
               '-recip', str(recipient), '-keyopt', 'rsa_padding_mode:oaep', '-keyopt', 'rsa_oaep_md:sha256']
    with output.open('xb') as encrypted:
        output.chmod(0o600)
        encrypted.write(CHUNKED)
        written = len(CHUNKED)
        current = incoming.read(CHUNK_BYTES)
        index = 0
        while True:
            following = incoming.read(CHUNK_BYTES)
            final = not following
            ciphertext = execute(command, input=struct.pack('>QB', index, final) + current)
            if len(ciphertext) > CHUNK_BYTES + CMS_OVERHEAD or 4 + len(ciphertext) > maximum - written:
                raise ValueError('Encrypted backup exceeds reviewed byte limit')
            encrypted.write(struct.pack('>I', len(ciphertext)))
            encrypted.write(ciphertext)
            written += 4 + len(ciphertext)
            index += 1
            if final: break
            if index > maximum // CHUNK_BYTES + 2: raise ValueError('Excessive encrypted chunk count')
            current = following
        encrypted.flush()
        os.fsync(encrypted.fileno())


def open_backup(path, maximum):
    flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_NONBLOCK', 0)
    descriptor = os.open(path, flags)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_size > maximum:
            raise ValueError('Encrypted backup must be a bounded ordinary file')
        return os.fdopen(descriptor, 'rb')
    except BaseException:
        os.close(descriptor)
        raise


def decrypt_chunks(backup, destination, encryption_key, maximum):
    command = ['openssl', 'cms', '-decrypt', '-binary', '-inform', 'DER', '-inkey', str(encryption_key)]
    with open_backup(backup, maximum) as incoming, destination.open('xb') as output:
        if incoming.read(len(CHUNKED)) != CHUNKED: raise ValueError('Unknown encrypted backup framing')
        total = 0
        consumed = len(CHUNKED)
        for index in range(maximum // CHUNK_BYTES + 3):
            raw_length = incoming.read(4)
            if len(raw_length) != 4: raise ValueError('Truncated encrypted backup')
            length = struct.unpack('>I', raw_length)[0]
            if not 0 < length <= CHUNK_BYTES + CMS_OVERHEAD: raise ValueError('Invalid encrypted chunk size')
            if 4 + length > maximum - consumed: raise ValueError('Encrypted input exceeds reviewed byte limit')
            consumed += 4 + length
            ciphertext = incoming.read(length)
            if len(ciphertext) != length: raise ValueError('Truncated encrypted chunk')
            validate_cms_chunk(ciphertext)
            plaintext = execute(command, input=ciphertext)
            if len(plaintext) < 9 or len(plaintext) > CHUNK_BYTES + 9: raise ValueError('Invalid authenticated chunk length')
            sequence, final = struct.unpack('>QB', plaintext[:9])
            body = plaintext[9:]
            if sequence != index or final not in (0, 1) or (not final and len(body) != CHUNK_BYTES):
                raise ValueError('Encrypted chunk order/final marker mismatch')
            if len(body) > maximum - total: raise ValueError('Decrypted backup exceeds reviewed byte limit')
            output.write(body)
            total += len(body)
            if final:
                if incoming.read(1): raise ValueError('Trailing encrypted backup data')
                return
        raise ValueError('Excessive encrypted chunk count')


def sign_encrypt(directory, output, recipient, signer_cert, signer_key, maximum):
    # Detached CMS streams its input digest without retaining the whole tar.
    with tempfile.TemporaryDirectory(prefix='artex-sign-') as temporary:
        staging = Path(temporary)
        staging.chmod(0o700)
        archive_path = staging / 'archive.tar'
        signature = staging / 'signature.cms'
        with tarfile.open(archive_path, mode='w') as archive:
            for path in sorted(directory.rglob('*')):
                if path.is_symlink(): raise ValueError('Unexpected staging symlink')
                archive.add(path, arcname=path.relative_to(directory).as_posix(), recursive=False)
        if archive_path.stat().st_size > maximum: raise ValueError('Tar exceeds reviewed backup byte limit')
        execute(['openssl', 'cms', '-sign', '-binary', '-outform', 'DER', '-md', 'sha256',
                 '-signer', str(signer_cert), '-inkey', str(signer_key), '-in', str(archive_path),
                 '-out', str(signature), '-keyopt', 'rsa_padding_mode:pss', '-keyopt', 'rsa_pss_saltlen:digest'], timeout=1800)
        signed = signature.read_bytes()
        if len(signed) > 1 << 20: raise ValueError('Oversized CMS signature')
        with archive_path.open('rb') as archive:
            incoming = JoinedReader(io.BytesIO(ENVELOPE + struct.pack('>I', len(signed)) + signed), archive)
            write_encrypted_chunks(incoming, output, recipient, maximum)


def authenticated_extract(backup, encryption_key, trusted_signer, directory, maximum):
    """Decrypt+verify fully before parsing any archive member."""
    expected_signer_pin = certificate_pin(trusted_signer)
    info = backup.stat()
    if not stat.S_ISREG(info.st_mode) or info.st_size > maximum:
        raise ValueError('Encrypted backup must be a bounded ordinary file')
    with tempfile.TemporaryDirectory(prefix='artex-auth-') as temporary:
        staging = Path(temporary)
        os.chmod(staging, 0o700)
        decrypted = staging / 'decrypted.envelope'
        signature = staging / 'signature.cms'
        archive = staging / 'authenticated.tar'
        decrypt_chunks(backup, decrypted, encryption_key, maximum)
        if decrypted.stat().st_size > maximum: raise ValueError('Signed backup exceeds its reviewed byte limit')
        with decrypted.open('rb') as envelope:
            if envelope.read(len(ENVELOPE)) != ENVELOPE: raise ValueError('Unknown backup envelope')
            length = envelope.read(4)
            if len(length) != 4: raise ValueError('Short backup envelope')
            length = struct.unpack('>I', length)[0]
            if not 0 < length <= 1 << 20: raise ValueError('Invalid signature length')
            signed = envelope.read(length)
            if len(signed) != length: raise ValueError('Truncated signature')
            signature.write_bytes(signed)
            with archive.open('xb') as destination:
                copy_bounded(envelope, destination, maximum)
        # Only the independently supplied signer is available to CMS verification;
        # embedded certificates and the system trust store cannot substitute it.
        execute(['openssl', 'cms', '-verify', '-binary', '-inform', 'DER', '-in', str(signature),
                 '-content', str(archive), '-nointern', '-certfile', str(trusted_signer),
                 '-CAfile', str(trusted_signer), '-no-CApath', '-no-CAstore', '-purpose', 'any',
                 '-verify_retcode', '-out', os.devnull], timeout=1800)
        with archive.open('rb') as incoming:
            extract_tar(incoming, directory, maximum)
    manifest_file = directory / 'manifest.json'
    if not manifest_file.is_file() or manifest_file.stat().st_size > MAX_MANIFEST:
        raise ValueError('Backup manifest missing or oversized')
    manifest = json.loads(manifest_file.read_text())
    if manifest.get('version') != 1 or manifest.get('signer_certificate_sha256') != expected_signer_pin:
        raise ValueError('Backup manifest version/signer pin mismatch')
    expected = manifest.get('files')
    if not isinstance(expected, dict) or len(expected) > MAX_FILES:
        raise ValueError('Invalid backup file manifest')
    actual = {path.relative_to(directory).as_posix(): digest_file(path)
              for path in directory.rglob('*') if path.is_file() and path != directory / 'manifest.json'}
    if actual != expected:
        raise ValueError('Backup file inventory, byte length or SHA256 mismatch')
    if 'postgres.dump' not in actual or 'files/private/jwt.key' not in actual or 'config/.env' not in actual or 'config/runtime-containers.json' not in actual:
        raise ValueError('Backup lacks required database, private authentication key or deployment configuration')
    if actual['files/private/jwt.key']['bytes'] != 32: raise ValueError('Backup JWT signing key has an invalid length')
    if manifest.get('broker_state_included') and not (directory / 'files/broker-state').is_dir():
        raise ValueError('Backup lacks its declared broker ledger state')
    return manifest


def pg_command(container, command):
    return ['docker', 'exec', '--user', 'postgres', container, 'sh', '-ec', command]


class RowDigest:
    def __init__(self):
        self.digest = hashlib.sha256()
        self.rows = 0
    def write(self, data):
        self.digest.update(data)
        self.rows += data.count(b'\n')
        return len(data)


def fingerprint(container, maximum=DEFAULT_MAX_BYTES):
    schema = execute(pg_command(container, 'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --schema-only --no-owner --no-acl --no-comments'))
    normalized = b'\n'.join(line for line in schema.splitlines() if not re.match(rb'^\\(?:un)?restrict ', line))
    listing = execute(pg_command(container, '''exec psql -XqAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "SELECT COALESCE(json_agg(json_build_object('schema',schemaname,'table',tablename) ORDER BY schemaname,tablename),'[]') FROM pg_tables WHERE schemaname NOT LIKE 'pg_%' AND schemaname <> 'information_schema'"'''))
    if len(listing) > 16 << 20: raise ValueError('Excessive database table inventory')
    tables = json.loads(listing)
    if len(tables) > 10000: raise ValueError('Excessive database table count')
    result = {'schema_sha256': hashlib.sha256(normalized).hexdigest(), 'tables': {}}
    for table in tables:
        quote = lambda value: '"' + value.replace('"', '""') + '"'
        identity = quote(table['schema']) + '.' + quote(table['table'])
        query = 'SET statement_timeout=600000; SET lock_timeout=30000; SET timezone=\'UTC\'; COPY (SELECT to_jsonb(t)::text FROM ' + identity + ' AS t ORDER BY to_jsonb(t)::text COLLATE "C") TO STDOUT'
        command = ['docker', 'exec', '-i', '--user', 'postgres', container, 'sh', '-ec',
                   'exec psql -XqAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1']
        sink = RowDigest()
        stream_command(command, sink, maximum, timeout=600, input_bytes=query.encode() + b';\n')
        result['tables'][identity] = {'rows': sink.rows, 'sha256': sink.digest.hexdigest()}
    return result


def verify_database(directory, manifest, image, maximum=DEFAULT_MAX_BYTES):
    nonce = secrets.token_hex(8)
    name = 'artex-backup-verify-' + nonce
    volume = name + '-disk'
    created = False
    try:
        docker(['run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true', '--entrypoint', '/bin/true', image])
        docker(['volume', 'create', '--label', 'artex.purpose=backup-verification', volume])
        created = True
        environment = directory.parent / ('verify-' + nonce + '.env')
        environment.write_text('POSTGRES_USER=artex_verify\nPOSTGRES_DB=artex_verify\nPOSTGRES_PASSWORD=' + secrets.token_hex(24) + '\n')
        environment.chmod(0o600)
        size = manifest.get('postgres_disk_bytes')
        if not isinstance(size, int) or size < 536870912 or size > 1099511627776 or size % 1048576:
            raise ValueError('Invalid bounded restore capacity')
        docker(['run', '-d', '--name', name, *runtime_arguments(volume, size, log_driver='none'), '--env-file', str(environment), image, 'postgres'])
        for _ in range(120):
            ready = subprocess.run(['docker', 'exec', name, 'pg_isready', '-h', '127.0.0.1', '-U', 'artex_verify'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if ready.returncode == 0: break
            time.sleep(.5)
        else: raise RuntimeError('Disposable PostgreSQL restore did not become ready')
        with (directory / 'postgres.dump').open('rb') as incoming:
            result = subprocess.run(['docker', 'exec', '-i', '--user', 'postgres', name, 'pg_restore',
                                     '-U', 'artex_verify', '-d', 'artex_verify', '--no-owner', '--no-acl', '--exit-on-error'],
                                    stdin=incoming, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=1800)
        if result.returncode: raise RuntimeError('Disposable database restore failed')
        if fingerprint(name, maximum) != manifest.get('database'):
            raise ValueError('Restored database schema/table content differs from the captured snapshot')
    finally:
        subprocess.run(['docker', 'rm', '-f', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if created:
            removed = subprocess.run(['docker', 'volume', 'rm', volume], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if removed.returncode: raise RuntimeError('Disposable backup verification volume requires explicit cleanup: ' + volume)


def compose_command(args):
    deployment = args.deployment_directory.resolve()
    command = ['docker', 'compose', '--project-directory', str(deployment)]
    if args.project: command += ['--project-name', args.project]
    for environment in ['.env', '.env.workers']:
        if (deployment / environment).is_file(): command += ['--env-file', str(deployment / environment)]
    files = args.compose_file or (['docker-compose.yml', 'compose.workers.yml'] if (deployment / '.env.workers').is_file() else ['docker-compose.yml'])
    for file in files:
        path = Path(file)
        command += ['-f', str(path if path.is_absolute() else deployment / path)]
    return command


@contextmanager
def quiesce(command, api):
    labels = json.loads(docker(['inspect', api]))[0]['Config'].get('Labels') or {}
    project = labels.get('com.docker.compose.project')
    if not project: raise ValueError('API is not a Compose-managed container')
    previous = []
    for service in ['artex', 'worker-broker']:
        ids = docker(['ps', '-q', '--no-trunc', '--filter', 'label=com.docker.compose.project=' + project,
                      '--filter', 'label=com.docker.compose.service=' + service]).decode().split()
        previous.extend(ids)
    if api not in previous: raise ValueError('API must be running before backup')
    try:
        docker(['stop', '--time', '120', *previous], timeout=180)
        yield
    finally:
        # Exact pre-existing IDs avoid reading changed environment files or
        # recreating/updating services while recovering from a failed backup.
        docker(['start', *previous], timeout=180)


def create_backup(args):
    output = args.output.resolve()
    if output.exists(): raise ValueError('Backup destination already exists')
    if output.is_relative_to(SOURCE): raise ValueError('Backups must be written outside the source checkout')
    recipient = private_file(args.recipient)
    signer = private_file(args.signing_cert)
    signing_key = private_file(args.signing_key, secret=True)
    encryption_key = private_file(args.encryption_key, secret=True)
    deployment = args.deployment_directory.resolve()
    if any(key.is_relative_to(deployment) for key in [signing_key, encryption_key]):
        raise ValueError('Private backup identities must be outside the deployment and its backed-up configuration')
    command = compose_command(args)
    api = execute(command + ['ps', '-q', 'artex']).decode().strip()
    database = execute(command + ['ps', '-q', 'postgres']).decode().strip()
    if not api or not database: raise ValueError('A running Compose API and PostgreSQL are required')
    inspected = json.loads(docker(['inspect', api]))[0]
    mounts = {mount['Destination']: mount for mount in inspected['Mounts']}
    volumes = {}
    for destination, label in [('/app/data', 'artifacts'), ('/app/private', 'private')]:
        if mounts.get(destination, {}).get('Type') != 'volume':
            raise ValueError('Backup requires the supported named state volumes')
        volumes[label] = mounts[destination]['Name']
    runtime = {'artex': {'image': inspected['Image'], 'environment': inspected['Config'].get('Env', []),
                         'entrypoint': inspected['Config'].get('Entrypoint'), 'command': inspected['Config'].get('Cmd')}}
    database_inspection = json.loads(docker(['inspect', database]))[0]
    runtime['postgres'] = {'image': database_inspection['Image'], 'environment': database_inspection['Config'].get('Env', []),
                           'entrypoint': database_inspection['Config'].get('Entrypoint'), 'command': database_inspection['Config'].get('Cmd')}
    project = (inspected['Config'].get('Labels') or {}).get('com.docker.compose.project')
    if not project: raise ValueError('Expected a Compose-managed application')
    brokers = docker(['ps', '-aq', '--no-trunc', '--filter', 'label=com.docker.compose.project=' + project,
                      '--filter', 'label=com.docker.compose.service=worker-broker']).decode().split()
    if len(brokers) > 1: raise ValueError('Ambiguous broker state; remove obsolete project containers deliberately')
    if brokers:
        broker = json.loads(docker(['inspect', brokers[0]]))[0]
        state = [mount for mount in broker['Mounts'] if mount['Destination'] == '/broker-state']
        if len(state) != 1 or state[0].get('Type') != 'volume': raise ValueError('Backup requires the supported named broker-state volume')
        volumes['broker-state'] = state[0]['Name']
        runtime['worker-broker'] = {'image': broker['Image'], 'environment': broker['Config'].get('Env', []),
                                    'entrypoint': broker['Config'].get('Entrypoint'), 'command': broker['Config'].get('Cmd')}
    database_image = json.loads(docker(['inspect', database]))[0]['Image']
    size = int(docker(['exec', database, 'stat', '-c', '%s', '/var/lib/artex-pgdisk/postgres.ext4']))
    output.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    partial = output.parent / ('.' + output.name + '.' + secrets.token_hex(8) + '.partial')
    try:
        with tempfile.TemporaryDirectory(prefix='artex-backup-') as temporary:
            root = Path(temporary)
            root.chmod(0o700)
            snapshot = root / 'snapshot'
            snapshot.mkdir(mode=0o700)
            with quiesce(command, api):
                state = fingerprint(database, args.max_bytes)
                with (snapshot / 'postgres.dump').open('xb') as dump:
                    (snapshot / 'postgres.dump').chmod(0o600)
                    dump_bytes = stream_command(pg_command(database, 'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --no-owner --no-acl'), dump, args.max_bytes)
                helper = ['run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--cap-add', 'DAC_READ_SEARCH',
                          '--log-driver', 'none', '--entrypoint', 'tar']
                for label, volume in volumes.items():
                    helper += ['--mount', 'type=volume,src=' + volume + ',dst=/snapshot/' + label + ',readonly']
                process = subprocess.Popen(['docker', *helper, database_image, '-C', '/snapshot', '-cf', '-', *volumes], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
                try:
                    files = snapshot / 'files'
                    files.mkdir(mode=0o700)
                    copied_bytes = extract_tar(process.stdout, files, args.max_bytes - dump_bytes)
                    remaining = args.max_bytes - dump_bytes - copied_bytes
                    if process.wait(timeout=600): raise RuntimeError('State volume snapshot failed')
                finally:
                    if process.poll() is None: process.kill(); process.wait()
                    process.stdout.close()
                for relative in CONFIG_FILES:
                    source = args.deployment_directory.resolve() / relative
                    if source.is_symlink(): raise ValueError('Deployment backup configuration must not be symlinks')
                    if source.exists():
                        if not source.is_file() or source.stat().st_size > 16 << 20: raise ValueError('Deployment configuration file is not bounded')
                        target = snapshot / 'config' / relative
                        target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                        with source.open('rb') as incoming, target.open('xb') as outgoing:
                            target.chmod(0o600)
                            copied = copy_bounded(incoming, outgoing, min(16 << 20, remaining))
                            remaining -= copied
                (snapshot / 'config').mkdir(exist_ok=True, mode=0o700)
                runtime_path = snapshot / 'config/runtime-containers.json'
                runtime_content = json.dumps(runtime, sort_keys=True)
                if len(runtime_content.encode()) > min(16 << 20, remaining): raise ValueError('Resolved configuration exceeds remaining backup byte budget')
                runtime_path.write_text(runtime_content)
                runtime_path.chmod(0o600)
                manifest = {'version': 1, 'created_at': datetime.now(timezone.utc).isoformat(), 'backup_id': secrets.token_hex(16),
                            'signer_certificate_sha256': certificate_pin(signer), 'postgres_image': database_image,
                            'postgres_disk_bytes': size, 'database': state, 'ephemeral_debug_traces_included': False,
                            'broker_state_included': 'broker-state' in volumes,
                            'scope': 'application database, named artifacts/private/broker-state volumes and selected/resolved deployment configuration',
                            'files': {path.relative_to(snapshot).as_posix(): digest_file(path) for path in snapshot.rglob('*') if path.is_file()}}
                if sum(item['bytes'] for item in manifest['files'].values()) > args.max_bytes: raise ValueError('Snapshot exceeds the reviewed byte limit')
                (snapshot / 'manifest.json').write_text(json.dumps(manifest, sort_keys=True))
            # The captured private snapshot is immutable to containers; restore
            # source availability before CPU/I/O-heavy signing and restore tests.
            sign_encrypt(snapshot, partial, recipient, signer, signing_key, args.max_bytes)
            verified = root / 'verified'
            verified.mkdir(mode=0o700)
            authenticated = authenticated_extract(partial, encryption_key, signer, verified, args.max_bytes)
            verify_database(verified, authenticated, database_image, args.max_bytes)
            # Exclusive hard-link publication avoids overwriting a concurrent backup.
            os.link(partial, output)
            partial.unlink()
            descriptor = os.open(output.parent, os.O_RDONLY)
            try: os.fsync(descriptor)
            finally: os.close(descriptor)
            print(json.dumps({'verified': True, 'backup': str(output), 'backup_id': manifest['backup_id'],
                              'sha256': digest_file(output)['sha256'], 'tables_verified': len(state['tables']),
                              'files_verified': len(manifest['files'])}, indent=2))
    finally:
        partial.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    keygen = sub.add_parser('keygen')
    keygen.add_argument('--directory', type=Path, required=True)
    create = sub.add_parser('create')
    create.add_argument('--project')
    create.add_argument('--deployment-directory', type=Path, default=SOURCE)
    create.add_argument('--compose-file', action='append')
    create.add_argument('--recipient', type=Path, required=True)
    create.add_argument('--signing-cert', type=Path, required=True)
    create.add_argument('--signing-key', type=Path, required=True)
    create.add_argument('--encryption-key', type=Path, required=True)
    create.add_argument('--output', type=Path, required=True)
    create.add_argument('--max-bytes', type=int, default=DEFAULT_MAX_BYTES)
    verify = sub.add_parser('verify')
    verify.add_argument('--backup', type=Path, required=True)
    verify.add_argument('--encryption-key', type=Path, required=True)
    verify.add_argument('--trusted-signing-cert', type=Path, required=True)
    verify.add_argument('--image', default='artex-local:postgres')
    verify.add_argument('--max-bytes', type=int, default=DEFAULT_MAX_BYTES)
    args = parser.parse_args()
    if args.command == 'keygen':
        directory = args.directory.resolve()
        if directory.is_relative_to(SOURCE): raise ValueError('Backup identity directory must be outside the source checkout')
        directory.mkdir(mode=0o700)
        for purpose in ['encryption', 'signing']:
            key = directory / (purpose + '-key.pem')
            cert = directory / (purpose + '-cert.pem')
            execute(['openssl', 'req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '3650', '-sha256',
                     '-subj', '/CN=ARTEX Backup ' + purpose, '-keyout', str(key), '-out', str(cert)])
            key.chmod(0o600)
            cert.chmod(0o600)
        print(json.dumps({'identity_directory': str(directory), 'trusted_signing_certificate_sha256': certificate_pin(directory / 'signing-cert.pem')}, indent=2))
        return
    if args.max_bytes < 1 << 20 or args.max_bytes > 1 << 40: raise ValueError('Reviewed byte limit must be between1MiB and1TiB')
    if args.command == 'create':
        create_backup(args)
    else:
        key = private_file(args.encryption_key, secret=True)
        signer = private_file(args.trusted_signing_cert)
        with tempfile.TemporaryDirectory(prefix='artex-verify-') as temporary:
            directory = Path(temporary) / 'snapshot'
            directory.mkdir(mode=0o700)
            manifest = authenticated_extract(args.backup.resolve(strict=True), key, signer, directory, args.max_bytes)
            verify_database(directory, manifest, args.image, args.max_bytes)
            print(json.dumps({'verified': True, 'backup_id': manifest['backup_id'], 'files_verified': len(manifest['files']), 'tables_verified': len(manifest['database']['tables'])}, indent=2))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, RuntimeError, subprocess.TimeoutExpired, OSError) as error:
        raise SystemExit(str(error))

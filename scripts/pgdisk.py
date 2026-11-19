#!/usr/bin/env python3
"""Explicit, offline migration into a NEW bounded PostgreSQL volume.

Never modifies/removes the source or automatically replaces an existing target.
Requires the same reviewed PostgreSQL16 image used by Compose.
"""
import argparse
import json
import re
import subprocess
import sys

CAPABILITIES = ['SYS_ADMIN', 'MKNOD', 'CHOWN', 'DAC_OVERRIDE', 'FOWNER', 'SETUID', 'SETGID', 'SETPCAP', 'MAC_OVERRIDE']


def docker(arguments, **options):
    result = subprocess.run(['docker', *arguments], capture_output=True, text=True, **options)
    if result.returncode:
        raise RuntimeError('Docker ' + arguments[0] + ' failed; inspect the named disposable migration container if retained')
    return result.stdout.strip()


def runtime_arguments(volume, size, *, log_driver="local"):
    if log_driver not in ('local', 'none'): raise ValueError('unsupported fixed logging policy')
    log_arguments = ['--log-driver', 'none'] if log_driver == 'none' else ['--log-driver', 'local', '--log-opt', 'max-size=10m', '--log-opt', 'max-file=3']
    arguments = ['--network', 'none', '--read-only', '--cap-drop', 'ALL']
    for capability in CAPABILITIES:
        arguments += ['--cap-add', capability]
    return arguments + ['--security-opt', 'no-new-privileges:true', '--security-opt', 'apparmor=unconfined', '--device', '/dev/loop-control',
                        '--device-cgroup-rule', 'b 7:* rwm', '--pids-limit', '256', '--memory', '1g',
                        '--cpus', '1', *log_arguments, '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=128m',
                        '--tmpfs', '/var/run/postgresql:rw,nosuid,nodev,noexec,size=16m,uid=70,gid=70',
                        '--tmpfs', '/var/lib/postgresql/data:rw,nosuid,nodev,noexec,size=1m,mode=000',
                        '--mount', 'type=volume,src=' + volume + ',dst=/var/lib/artex-pgdisk',
                        '-e', 'ARTEX_PG_DISK_BYTES=' + str(size), '-e', 'PGDATA=/var/lib/postgresql/bounded/data']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--legacy-volume', required=True)
    parser.add_argument('--project', required=True)
    parser.add_argument('--bytes', type=int, default=8589934592)
    parser.add_argument('--image', default='artex-local:postgres')
    parser.add_argument('--confirmation', required=True)
    args = parser.parse_args()
    if args.confirmation != 'MIGRATE STOPPED POSTGRESQL VOLUME':
        parser.error('confirmation must be MIGRATE STOPPED POSTGRESQL VOLUME')
    if not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,62}', args.project):
        parser.error('project must be a simple lowercase Compose project name')
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}', args.legacy_volume):
        parser.error('invalid source volume name')
    if args.bytes < 536870912 or args.bytes > 1099511627776 or args.bytes % 1048576:
        parser.error('bytes must be whole MiB between512MiB and1TiB')
    target = args.project + '_pgdisk'
    if target == args.legacy_volume:
        parser.error('source and target must differ')
    docker(['volume', 'inspect', args.legacy_volume])
    active = docker(['ps', '-q', '--filter', 'volume=' + args.legacy_volume])
    if active:
        raise RuntimeError('Source volume has running containers. Stop all producers and PostgreSQL cleanly first.')
    existing = subprocess.run(['docker', 'volume', 'inspect', target], capture_output=True)
    if existing.returncode == 0:
        raise RuntimeError('Target already exists; inspect it rather than replacing it: ' + target)
    image = json.loads(docker(['image', 'inspect', args.image]))[0]['Id']
    docker(['run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true', '--entrypoint', '/bin/true', image])
    docker(['volume', 'create', '--label', 'com.docker.compose.project=' + args.project,
            '--label', 'com.docker.compose.volume=pgdisk', '--label', 'artex.purpose=bounded-postgresql', target])
    # Deliberately retain the new volume on every failure: it may contain a useful
    # verified/partial copy, and deleting a database is never an implicit cleanup.
    output = docker(['run', '--rm', *runtime_arguments(target, args.bytes),
                     '--mount', 'type=volume,src=' + args.legacy_volume + ',dst=/legacy-pgdata,readonly',
                     '--entrypoint', '/usr/local/bin/artex-postgres-entrypoint', image, '--migrate'], timeout=3600)
    if 'migration=verified-byte-copy' not in output:
        raise RuntimeError('Migration did not report complete verification; new target preserved for inspection: ' + target)
    print(json.dumps({'migrated': True, 'source_retained': args.legacy_volume, 'new_target': target,
                      'bytes': args.bytes, 'image': image}, indent=2))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        sys.exit(str(error))

#!/usr/bin/env python3
"""Inspect or explicitly release the fixed recovery reserve of an OFFLINE PG disk.

Never formats a volume, deletes database files, resizes an image or starts PG.
"""
import argparse
import json
import re
import subprocess

from pgdisk import docker, runtime_arguments


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--volume', required=True)
    parser.add_argument('--bytes', type=int, default=8589934592)
    parser.add_argument('--image', default='artex-local:postgres')
    parser.add_argument('--release-recovery-reserve', action='store_true')
    parser.add_argument('--confirmation', default='')
    args = parser.parse_args()
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}', args.volume): parser.error('invalid volume name')
    if args.bytes < 536870912 or args.bytes > 1099511627776 or args.bytes % 1048576:
        parser.error('bytes must be whole MiB between512MiB and1TiB')
    if args.release_recovery_reserve and args.confirmation != 'RELEASE GENERATED POSTGRESQL RECOVERY RESERVE':
        parser.error('explicit confirmation required to release the generated reserve')
    docker(['volume', 'inspect', args.volume])
    if docker(['ps', '-q', '--filter', 'volume=' + args.volume]):
        raise RuntimeError('Stop all containers using this disk before offline maintenance')
    image = json.loads(docker(['image', 'inspect', args.image]))[0]['Id']
    # Prove prior initialization through a read-only mount before invoking the
    # bootstrap, so a mistaken empty volume name cannot initialize a new disk.
    docker(['run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL',
            '--log-driver', 'local', '--log-opt', 'max-size=2m', '--log-opt', 'max-file=2',
            '--mount', 'type=volume,src=' + args.volume + ',dst=/disk,readonly',
            '--entrypoint', '/bin/sh', image, '-ec', 'test -f /disk/format-v1; test -f /disk/postgres.ext4'])
    mode = '--release-recovery-reserve' if args.release_recovery_reserve else '--inspect'
    print(docker(['run', '--rm', *runtime_arguments(args.volume, args.bytes), image, mode], timeout=180))


if __name__ == '__main__':
    try: main()
    except (RuntimeError, subprocess.TimeoutExpired) as error: raise SystemExit(str(error))

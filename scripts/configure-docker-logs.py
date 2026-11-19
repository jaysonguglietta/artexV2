#!/usr/bin/env python3
"""Merge bounded Docker/journald logging on the dedicated Linux Docker VM.

Run as root INSIDE the VM. Preserves unrelated daemon options, backs up existing
config, and never restarts services or deletes logs automatically.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import tempfile

DAEMON_OPTIONS = {'log-driver': 'local', 'log-opts': {'max-size': '10m', 'max-file': '3', 'compress': 'true'}}
JOURNAL = '''# Managed ARTEX dedicated-VM capacity policy.
[Journal]
SystemMaxUse=256M
RuntimeMaxUse=64M
SystemKeepFree=1G
RuntimeKeepFree=64M
MaxRetentionSec=7day
'''


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result: raise ValueError('Duplicate daemon configuration key')
        result[key] = value
    return result


def merged_daemon(raw):
    existing = json.loads(raw, object_pairs_hook=unique_object) if raw else {}
    if not isinstance(existing, dict): raise ValueError('Docker daemon configuration must be an object')
    # Log options are driver-specific; retaining an option from a different
    # driver would make Docker fail startup. Other top-level options are kept.
    existing.update(DAEMON_OPTIONS)
    return json.dumps(existing, indent=2, sort_keys=True) + '\n'


def install(path, value):
    if path.is_symlink(): raise ValueError('Refusing symlink configuration path')
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
    encoded = value.encode()
    if path.exists() and path.read_bytes() == encoded: return False
    if path.exists():
        stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
        backup = path.with_name(path.name + '.before-artex-' + stamp)
        with backup.open('xb') as copy:
            os.chmod(backup, 0o600)
            copy.write(path.read_bytes())
            copy.flush()
            os.fsync(copy.fileno())
    descriptor, temporary = tempfile.mkstemp(prefix='.artex-logging-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'wb') as output:
            os.chmod(temporary, 0o600)
            output.write(encoded)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try: os.fsync(directory)
        finally: os.close(directory)
    finally:
        Path(temporary).unlink(missing_ok=True)
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--daemon-json', type=Path, default=Path('/etc/docker/daemon.json'))
    parser.add_argument('--journald-dropin', type=Path, default=Path('/etc/systemd/journald.conf.d/90-artex-limits.conf'))
    args = parser.parse_args()
    raw = args.daemon_json.read_text() if args.daemon_json.exists() else ''
    daemon = merged_daemon(raw)
    if not args.apply:
        print(json.dumps({'planned': DAEMON_OPTIONS, 'journal_disk_bytes': 256 << 20,
                          'journal_runtime_bytes': 64 << 20, 'restart_required': True}, indent=2))
        return
    if os.uname().sysname != 'Linux' or os.geteuid() != 0:
        raise ValueError('Apply requires root inside the dedicated Linux Docker VM')
    changed = [str(path) for path, content in [(args.daemon_json, daemon), (args.journald_dropin, JOURNAL)] if install(path, content)]
    print(json.dumps({'configured': changed, 'services_restarted': False,
                      'next_step': 'When all fixtures/services are stopped, restart docker and systemd-journald, then recreate existing containers and inspect their LogConfig.'}, indent=2))


if __name__ == '__main__':
    try: main()
    except (ValueError, OSError) as error: raise SystemExit(str(error))

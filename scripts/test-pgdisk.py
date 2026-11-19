#!/usr/bin/env python3
"""Exercise bounded PostgreSQL on new disposable volumes; never use a user DSN."""
import argparse
import json
from pathlib import Path
import secrets
import subprocess
import tempfile
import time

from pgdisk import runtime_arguments

POSTGRES_APPARMOR_LABELS = (
    "docker-default (enforce)",
    "docker-default//&unconfined (enforce)",
)


def run(arguments, *, check=True, timeout=90):
    result = subprocess.run(['docker', *arguments], capture_output=True, text=True, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError('Docker fixture failed: ' + arguments[0] + ': ' + result.stderr[-1000:])
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default='artex-local:postgres')
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    nonce = secrets.token_hex(8)
    name = 'artex-pgdisk-test-' + nonce
    volume = name + '-disk'
    checks = []
    failure = None
    created = False
    def check(label, condition):
        checks.append({'check': label, 'passed': bool(condition)})
        if not condition: raise RuntimeError(label)
        print('PASS ' + label, flush=True)
    def ready():
        for _ in range(60):
            if run(['exec', name, 'pg_isready', '-h', '127.0.0.1', '-U', 'fixture'], check=False).returncode == 0: return
            time.sleep(.5)
        logs = run(['logs', '--tail', '15', name], check=False)
        raise RuntimeError('PostgreSQL fixture startup failed: ' + logs.stdout + logs.stderr)
    def sql(query, check=True):
        return run(['exec', '--user', 'postgres', name, 'psql', '-U', 'fixture', '-d', 'fixture', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query], check=check)
    try:
        run(['run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true', '--entrypoint', '/bin/true', args.image])
        run(['volume', 'create', '--label', 'artex.purpose=pgdisk-security-test', volume])
        created = True
        run(['run', '-d', '--name', name, *runtime_arguments(volume, 536870912),
             '-e', 'POSTGRES_USER=fixture', '-e', 'POSTGRES_DB=fixture',
             '-e', 'POSTGRES_PASSWORD=synthetic-disposable-only', args.image, 'postgres'])
        ready()
        server = run(['exec', name, 'head', '-n', '1', '/var/lib/postgresql/bounded/data/postmaster.pid']).stdout.strip()
        statuses = [run(['exec', name, 'cat', '/proc/' + pid + '/status']).stdout for pid in ['1', server]]
        def safe(status):
            fields = {line.split(':', 1)[0]: line.split(':', 1)[1].split() for line in status.splitlines() if ':' in line}
            return (fields.get('Uid') == ['70'] * 4 and fields.get('NoNewPrivs') == ['1']
                    and all(fields.get(cap) == ['0000000000000000'] for cap in ['CapEff','CapPrm','CapInh','CapBnd','CapAmb']))
        check('Supervisor and PostgreSQL are UID70 with all capability sets zero and NNP', all(safe(status) for status in statuses))
        profiles = [run(['exec', name, 'cat', '/proc/' + pid + '/attr/apparmor/current']).stdout.removesuffix('\n') for pid in ['1', server]]
        check('Supervisor and PostgreSQL re-enter the enforcing Docker AppArmor profile', all(profile in POSTGRES_APPARMOR_LABELS for profile in profiles))
        filesystem = run(['exec', name, 'findmnt', '-n', '-o', 'FSTYPE', '--target', '/var/lib/postgresql/bounded']).stdout.strip()
        check('PGDATA is an ext4 filesystem, not an unbounded volume directory', filesystem == 'ext4')
        protected = run(['exec', '--user', 'postgres', name, 'test', '-r', '/var/lib/artex-pgdisk/postgres.ext4'], check=False)
        check('PostgreSQL cannot open or grow its backing image', protected.returncode != 0)
        protected_reserve = run(['exec', '--user', 'postgres', name, 'rm', '/var/lib/postgresql/bounded/.artex-recovery-reserve'], check=False)
        check('PostgreSQL cannot consume or delete the protected recovery reserve', protected_reserve.returncode != 0)
        sql('CREATE TABLE durable_probe(value integer); INSERT INTO durable_probe VALUES(42)')
        blocked = run(['run', '--rm', *runtime_arguments(volume, 536870912), args.image, '--inspect'], check=False)
        check('A concurrent second mount of the same backing image is refused', blocked.returncode != 0)
        mismatch = run(['run', '--rm', *runtime_arguments(volume, 1073741824), args.image, '--inspect'], check=False)
        check('Existing filesystem is never resized or reformatted for a changed bound', mismatch.returncode != 0)
        legacy_write = run(['exec', '--user', 'postgres', name, 'touch', '/var/lib/postgresql/data/escape'], check=False)
        check('The inherited legacy data volume cannot bypass the PostgreSQL disk bound', legacy_write.returncode != 0)
        filled = run(['exec', '--user', 'postgres', name, 'sh', '-c',
                      'LC_ALL=C dd if=/dev/zero of=/var/lib/postgresql/bounded/data/quota-fixture bs=1048576 2>/tmp/quota-error; status=$?; test "$status" -ne 0 && grep -q "No space left on device" /tmp/quota-error'], timeout=90)
        check('The kernel refuses additional writes at the fixed filesystem boundary', filled.returncode == 0)
        size = run(['exec', name, 'stat', '-c', '%s', '/var/lib/artex-pgdisk/postgres.ext4']).stdout.strip()
        check('Disk exhaustion does not enlarge the persistent backing image', size == '536870912')
        growth = sql("CREATE TABLE quota_growth_probe AS SELECT repeat('x',8192) AS value FROM generate_series(1,100)", check=False)
        check('PostgreSQL refuses new persistent database growth at disk exhaustion', growth.returncode != 0)
        # A full WAL filesystem may stop PostgreSQL. Recover only this explicitly
        # created test marker offline; never delete database/WAL files to reclaim.
        run(['stop', '--time', '20', name], check=False)
        released = run(['run', '--rm', *runtime_arguments(volume, 536870912), args.image, '--release-recovery-reserve'])
        check('Explicit offline recovery frees only the generated zero reserve', 'user_data_deleted=false' in released.stdout)
        run(['start', name])
        ready()
        check('Previously committed data survives disk-full shutdown and offline reclamation', sql('SELECT value FROM durable_probe').stdout.strip() == '42')
        run(['exec', '--user', 'postgres', name, 'rm', '/var/lib/postgresql/bounded/data/quota-fixture'])
        sql('INSERT INTO durable_probe VALUES(43)')
        run(['restart', '--time', '20', name])
        ready()
        check('New committed writes remain durable across another restart', sql('SELECT sum(value) FROM durable_probe').stdout.strip() == '85')
        absent = run(['exec', name, 'test', '-e', '/var/lib/postgresql/bounded/.artex-recovery-reserve'], check=False)
        check('Released recovery space is never recreated automatically on restart', absent.returncode != 0)
    except Exception as error:
        failure = str(error)
    finally:
        cleanup = run(['rm', '-f', name], check=False).returncode == 0
        if created:
            cleanup = run(['volume', 'rm', volume], check=False).returncode == 0 and cleanup
        checks.append({'check': 'Only the disposable database container and backing volume were removed', 'passed': cleanup})
    report = {'passed': failure is None and all(item['passed'] for item in checks), 'checks': checks}
    if failure: report['failure'] = failure
    output = args.report or Path(tempfile.mkdtemp(prefix='artex-pgdisk-results-')) / 'results.json'
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(str(output), flush=True)
    if not report['passed']: raise SystemExit(1)


if __name__ == '__main__': main()

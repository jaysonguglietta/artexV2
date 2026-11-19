#!/usr/bin/env python3
"""Test explicit legacy-volume migration with synthetic PostgreSQL data only."""
import argparse
import json
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import time

from pgdisk import runtime_arguments


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default='artex-local:postgres')
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    project = 'artex-migration-test-' + secrets.token_hex(7)
    legacy, target = project + '-legacy', project + '_pgdisk'
    old, new = project + '-old', project + '-new'
    checks = []
    failure = None
    def docker(arguments, check=True):
        result = subprocess.run(['docker', *arguments], capture_output=True, text=True, timeout=120)
        if check and result.returncode: raise RuntimeError('Synthetic migration Docker operation failed: ' + arguments[0] + ': ' + result.stderr[-500:])
        return result
    def check(label, condition):
        checks.append({'check': label, 'passed': bool(condition)})
        if not condition: raise RuntimeError(label)
        print('PASS ' + label, flush=True)
    def ready(container):
        for _ in range(60):
            if docker(['exec', container, 'pg_isready', '-h', '127.0.0.1', '-U', 'fixture'], False).returncode == 0: return
            time.sleep(.5)
        raise RuntimeError('Synthetic migration database did not start')
    command = [sys.executable, str(Path(__file__).with_name('pgdisk.py')), '--legacy-volume', legacy,
               '--project', project, '--bytes', '536870912', '--image', args.image,
               '--confirmation', 'MIGRATE STOPPED POSTGRESQL VOLUME']
    try:
        docker(['volume', 'create', '--label', 'artex.purpose=migration-test', legacy])
        docker(['run', '-d', '--name', old, '--network', 'none', '--log-driver', 'local', '--log-opt', 'max-size=2m', '--log-opt', 'max-file=2',
                '--mount', 'type=volume,src=' + legacy + ',dst=/var/lib/postgresql/data',
                '-e', 'PGDATA=/var/lib/postgresql/data', '-e', 'POSTGRES_USER=fixture', '-e', 'POSTGRES_DB=fixture',
                '-e', 'POSTGRES_PASSWORD=synthetic-fixture-only', '--entrypoint', '/usr/local/bin/docker-entrypoint.sh', args.image, 'postgres'])
        ready(old)
        docker(['exec', old, 'psql', '-U', 'fixture', '-d', 'fixture', '-v', 'ON_ERROR_STOP=1', '-c',
                'CREATE TABLE migration_probe(value integer); INSERT INTO migration_probe VALUES(2468)'])
        refused = subprocess.run(command, capture_output=True, text=True, timeout=120)
        check('An active source volume is refused before target creation', refused.returncode != 0 and docker(['volume', 'inspect', target], False).returncode != 0)
        docker(['stop', '--time', '60', old])
        copied = subprocess.run(command, capture_output=True, text=True, timeout=180)
        check('A cleanly stopped legacy database is copied and byte-verified into a new bounded target', copied.returncode == 0 and json.loads(copied.stdout).get('source_retained') == legacy)
        docker(['run', '-d', '--name', new, *runtime_arguments(target, 536870912), '-e', 'POSTGRES_USER=fixture', '-e', 'POSTGRES_DB=fixture',
                '-e', 'POSTGRES_PASSWORD=synthetic-fixture-only', args.image, 'postgres'])
        ready(new)
        data = docker(['exec', new, 'psql', '-U', 'fixture', '-d', 'fixture', '-At', '-c', 'SELECT value FROM migration_probe']).stdout.strip()
        check('The bounded migrated database preserves original committed rows', data == '2468')
        repeated = subprocess.run(command, capture_output=True, text=True, timeout=120)
        check('An existing migration target is refused rather than overwritten', repeated.returncode != 0)
        docker(['start', old])
        ready(old)
        data = docker(['exec', old, 'psql', '-U', 'fixture', '-d', 'fixture', '-At', '-c', 'SELECT value FROM migration_probe']).stdout.strip()
        check('The original legacy volume remains independently readable after migration', data == '2468')
        docker(['stop', '--time', '60', new])
        docker(['run', '--rm', '--network', 'none', '--cap-drop', 'ALL', '--log-driver', 'local', '--log-opt', 'max-size=2m', '--log-opt', 'max-file=2',
                '--mount', 'type=volume,src=' + target + ',dst=/disk', '--entrypoint', 'sh', args.image, '-ec', "printf 'synthetic pending marker\\n' > /disk/migration-pending"])
        docker(['start', new])
        time.sleep(1)
        running = docker(['inspect', new, '--format', '{{.State.Running}}']).stdout.strip()
        logs = docker(['logs', '--tail', '10', new], False)
        check('A durable pending marker blocks database startup after interrupted migration', running == 'false' and 'incomplete migration' in logs.stderr + logs.stdout)
    except Exception as error:
        failure = str(error)
    finally:
        cleanup = docker(['rm', '-f', old, new], False).returncode == 0
        for volume in [legacy, target]:
            if docker(['volume', 'inspect', volume], False).returncode == 0:
                cleanup = docker(['volume', 'rm', volume], False).returncode == 0 and cleanup
        checks.append({'check': 'Only synthetic migration containers and volumes were removed', 'passed': cleanup})
    report = {'passed': failure is None and all(item['passed'] for item in checks), 'checks': checks}
    if failure: report['failure'] = failure
    path = args.report or Path(tempfile.mkdtemp(prefix='artex-migration-results-')) / 'results.json'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + '\n')
    print(path, flush=True)
    if not report['passed']: raise SystemExit(1)


if __name__ == '__main__': main()

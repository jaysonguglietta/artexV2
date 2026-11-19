#!/usr/bin/env python3
"""Prove real health/client privilege drops in a private disposable PG deployment."""
import argparse
import copy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
ENTRYPOINT = '/usr/local/bin/artex-postgres-entrypoint'
HEALTHCHECK = ['CMD', ENTRYPOINT, '--healthcheck']
CAP_FIELDS = ('CapEff', 'CapPrm', 'CapInh', 'CapBnd', 'CapAmb')
BOOTSTRAP_CAPS = {'SYS_ADMIN', 'MKNOD', 'CHOWN', 'DAC_OVERRIDE', 'FOWNER',
                  'SETUID', 'SETGID', 'SETPCAP', 'MAC_OVERRIDE'}


def docker(arguments, *, check=True, timeout=90, environment=None):
    result = subprocess.run(['docker', *arguments], capture_output=True, text=True,
                            timeout=timeout, env=environment)
    if check and result.returncode:
        raise RuntimeError('Docker client-security fixture failed: ' + arguments[0])
    return result


def safe_client_status(status):
    fields = {line.split(':', 1)[0]: line.split(':', 1)[1].split()
              for line in status.splitlines() if ':' in line}
    return (fields.get('Uid') == ['70'] * 4 and fields.get('Gid') == ['70'] * 4
            and fields.get('Groups') == []
            and fields.get('NoNewPrivs') == ['1']
            and all(fields.get(field) == ['0000000000000000'] for field in CAP_FIELDS))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default='artex-local:postgres')
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    nonce = secrets.token_hex(8)
    project, name, volume = 'artex-pg-clients-' + nonce, 'artex-pg-client-' + nonce, 'artex-pg-client-disk-' + nonce
    checks, failure, created_volume, owned_container, query, image = [], None, False, False, None, None
    sources = ('postgres/bounded-entrypoint.sh', 'docker-compose.yml', 'scripts/test-postgres-clients.py')
    source_hashes = {source: hashlib.sha256((ROOT / source).read_bytes()).hexdigest() for source in sources}
    def check(label, condition):
        checks.append({'check': label, 'passed': bool(condition)})
        if not condition:
            raise RuntimeError(label)
        print('PASS ' + label, flush=True)
    try:
        image = json.loads(docker(['image', 'inspect', '--', args.image]).stdout)[0]['Id']
        if docker(['ps', '-aq', '--filter', 'name=^/' + name + '$']).stdout.strip():
            raise RuntimeError('Refusing a pre-existing fixture container')
        existing = docker(['volume', 'ls', '--filter', 'name=' + volume, '--format', '{{.Name}}']).stdout.splitlines()
        if volume in existing:
            raise RuntimeError('Refusing a pre-existing fixture volume')
        with tempfile.TemporaryDirectory(prefix='artex-pg-client-config-') as temporary:
            private = Path(temporary)
            environment = dict(os.environ, POSTGRES_PASSWORD='synthetic-client-fixture-only', ARTEX_PG_DISK_BYTES='536870912')
            rendered = json.loads(docker(['compose', '--env-file', os.devnull, '-f', str(ROOT / 'docker-compose.yml'),
                                          'config', '--format', 'json'], environment=environment).stdout)
            service = copy.deepcopy(rendered['services']['postgres'])
            check('Production healthcheck uses the fixed trusted client dispatcher', service['healthcheck']['test'] == HEALTHCHECK)
            check('Production bootstrap includes only the reviewed MAC transition capability',
                  set(service['cap_add']) == BOOTSTRAP_CAPS and service['cap_drop'] == ['ALL'])
            service.pop('build', None)
            service.pop('networks', None)
            service.pop('ports', None)
            service.update(image=image, container_name=name, network_mode='none', restart='no',
                           environment={'POSTGRES_USER': 'fixture', 'POSTGRES_DB': 'fixture',
                                        'POSTGRES_PASSWORD': 'synthetic-client-fixture-only',
                                        'ARTEX_PG_DISK_BYTES': '536870912', 'PGDATA': '/var/lib/postgresql/bounded/data'},
                           logging={'driver': 'none'})
            check('Client proof uses only the production bounded PostgreSQL state mount',
                  len(service['volumes']) == 1 and service['volumes'][0]['target'] == '/var/lib/artex-pgdisk'
                  and service['volumes'][0]['type'] == 'volume')
            service['volumes'][0]['source'] = 'fixture-disk'
            service['healthcheck'].update(interval='1s', start_period='30s')
            config = {'services': {'postgres': service}, 'volumes': {'fixture-disk': {'external': True, 'name': volume}}}
            config_file = private / 'compose.json'
            config_file.write_text(json.dumps(config))
            config_file.chmod(0o600)
            docker(['volume', 'create', '--label', 'artex.purpose=postgres-client-security-test', volume])
            created_volume = True
            owned_container = True
            docker(['compose', '--env-file', os.devnull, '--project-directory', str(private), '-p', project,
                    '-f', str(config_file), 'up', '--detach', '--no-build', '--pull', 'never'], timeout=180)
            health = None
            for _ in range(120):
                inspected = json.loads(docker(['inspect', name]).stdout)[0]
                health = inspected['State'].get('Health', {})
                if health.get('Status') == 'healthy':
                    break
                if not inspected['State']['Running']:
                    raise RuntimeError('Private PostgreSQL client fixture stopped during startup')
                time.sleep(.5)
            check('Actual Docker healthcheck executes successfully through the production privilege-drop guard',
                  health.get('Status') == 'healthy' and health.get('Log')
                  and health['Log'][-1]['ExitCode'] == 0
                  and inspected['Image'] == image
                  and inspected['Config']['Healthcheck']['Test'] == HEALTHCHECK)
            for uid in ('0:0', '70:70'):
                marker = '/tmp/refused-client-' + uid.replace(':', '-')
                refused = docker(['exec', '--user', uid, name, ENTRYPOINT, '--run-client', '/bin/sh', '-ec',
                                  'printf refused > ' + marker], check=False)
                absent = docker(['exec', name, 'test', '-e', marker], check=False)
                check('Direct Docker exec UID' + uid + ' cannot bypass the post-drop client guard',
                      refused.returncode != 0 and absent.returncode == 1)
            query = subprocess.Popen(['docker', 'exec', '--user', '0:0', name, ENTRYPOINT, '--client',
                                      '/usr/local/bin/psql', '-h', '/var/run/postgresql', '-U', 'fixture', '-d', 'fixture',
                                      '-XAtw', '-v', 'ON_ERROR_STOP=1', '-c', 'SELECT pg_sleep(5)'],
                                     stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            probe = '''for path in /proc/[0-9]*/comm; do
  IFS= read -r command < "$path" || continue
  [ "$command" = psql ] || continue
  pid=${path#/proc/}; pid=${pid%/comm}
  printf 'PID=%s\\n' "$pid"
  cat "/proc/$pid/status"
  printf 'APPARMOR='
  cat "/proc/$pid/attr/apparmor/current"
  printf '\\nEND\\n'
done'''
            observed = ''
            deadline = time.monotonic() + 4
            while time.monotonic() < deadline:
                observed = docker(['exec', name, '/bin/sh', '-ec', probe]).stdout
                if 'PID=' in observed:
                    break
                if query.poll() is not None:
                    break
                time.sleep(.05)
            check('The actual active psql process is UID/GID70 with all five capability sets zero and NNP',
                  observed.count('PID=') == 1 and safe_client_status(observed))
            check('The actual active psql process has the single enforcing Docker AppArmor profile',
                  'APPARMOR=docker-default (enforce)\n' in observed)
            query.communicate(timeout=15)
            check('The confined client completes a real database request', query.returncode == 0)
            check('Reviewed production and test source stayed unchanged throughout the live proof',
                  source_hashes == {source: hashlib.sha256((ROOT / source).read_bytes()).hexdigest() for source in sources})
    except Exception as error:
        failure = str(error)
    finally:
        if query is not None and query.poll() is None:
            query.kill()
            query.communicate(timeout=10)
        cleanup = True
        try:
            if owned_container:
                present = docker(['ps', '-aq', '--filter', 'name=^/' + name + '$'], check=False)
                cleanup = present.returncode == 0
                if present.stdout.strip():
                    cleanup = docker(['rm', '-f', name], check=False).returncode == 0 and cleanup
            if created_volume:
                cleanup = docker(['volume', 'rm', volume], check=False).returncode == 0 and cleanup
        except (OSError, subprocess.SubprocessError):
            cleanup = False
        checks.append({'check': 'Only the private client-proof container and volume were removed', 'passed': cleanup})
    report = {'passed': failure is None and all(item['passed'] for item in checks), 'checks': checks,
              'completed_at': datetime.now(timezone.utc).isoformat(), 'image_id': image, 'source_sha256': source_hashes}
    if failure:
        report['failure'] = failure
    output = args.report or Path(tempfile.mkdtemp(prefix='artex-pg-client-results-')) / 'results.json'
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(output, flush=True)
    if not report['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()

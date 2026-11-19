#!/usr/bin/env python3
"""Verify an actual encrypted backup/isolated restore using a new Compose fixture."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile

SOURCE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(Path(__file__).resolve().parent))
spec = importlib.util.spec_from_file_location('compose_fixture', Path(__file__).with_name('test-compose.py'))
compose = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compose)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    watched = ['scripts/artex-backup.py', 'scripts/backup_crypto.py', 'scripts/pgdisk.py',
               'scripts/test-compose.py', 'postgres/bounded-entrypoint.sh', 'docker-compose.yml', 'compose.workers.yml']
    before = {name: hashlib.sha256((SOURCE / name).read_bytes()).hexdigest() for name in watched}
    fixture = compose.Fixture(Path(tempfile.mkdtemp(prefix='.worker-fixture.backup-', dir=SOURCE)))
    completed = False
    failure = None
    try:
        fixture.setup_and_run()
        database = fixture.run(fixture.command + ['ps', '-q', 'postgres']).stdout.strip()
        fixture.run(['docker', 'exec', database, 'psql', '-U', 'artex', '-d', 'artex', '-v', 'ON_ERROR_STOP=1', '-c',
                     "CREATE TABLE backup_fixture(id integer PRIMARY KEY, secret text); INSERT INTO backup_fixture VALUES(7,'synthetic private evidence only');"])
        with tempfile.TemporaryDirectory(prefix='artex-backup-integration-') as temporary:
            root = Path(temporary)
            root.chmod(0o700)
            identity = root / 'identity'
            fixture.run([sys.executable, str(SOURCE / 'scripts/artex-backup.py'), 'keygen', '--directory', str(identity)])
            backup = root / 'fixture.cms'
            created = fixture.run([sys.executable, str(SOURCE / 'scripts/artex-backup.py'), 'create',
                                  '--deployment-directory', str(fixture.directory), '--project', fixture.project,
                                  '--compose-file', 'docker-compose.yml', '--compose-file', 'compose.workers.yml',
                                  '--compose-file', 'smoke.override.yml', '--recipient', str(identity / 'encryption-cert.pem'),
                                  '--signing-cert', str(identity / 'signing-cert.pem'), '--signing-key', str(identity / 'signing-key.pem'),
                                  '--encryption-key', str(identity / 'encryption-key.pem'), '--output', str(backup),
                                  '--max-bytes', str(256 << 20)], timeout=600)
            result = json.loads(created.stdout)
            fixture.check('Actual snapshot is encrypted, pinned-signer authenticated and restored in a separate bounded PostgreSQL', result.get('verified') is True and result['tables_verified'] > 1 and result['files_verified'] >= 3)
            verified = fixture.run([sys.executable, str(SOURCE / 'scripts/artex-backup.py'), 'verify', '--backup', str(backup),
                                   '--encryption-key', str(identity / 'encryption-key.pem'), '--trusted-signing-cert', str(identity / 'signing-cert.pem'),
                                   '--max-bytes', str(256 << 20)], timeout=600)
            fixture.check('Standalone verification repeats schema/table/file hashes and private-key/config scope', json.loads(verified.stdout).get('verified') is True)
            for service in ['artex', 'worker-broker']:
                identity_container = fixture.run(fixture.command + ['ps', '-q', '--status', 'running', service]).stdout.strip()
                fixture.check('Backup restores prior ' + service + ' availability', bool(identity_container))
            unchanged = fixture.run(['docker', 'exec', database, 'psql', '-U', 'artex', '-d', 'artex', '-At', '-c', 'SELECT id FROM backup_fixture']).stdout.strip()
            fixture.check('Disposable restore never overwrites or replaces the source database', unchanged == '7')
        completed = True
    except Exception as error:
        failure = fixture.redact(str(error))
        diagnostic = fixture.postgres_failure_diagnostics()
        if diagnostic:
            failure += "\n" + diagnostic
        print("FAILED: " + failure, file=sys.stderr, flush=True)
    finally:
        errors = fixture.cleanup()
    after = {name: hashlib.sha256((SOURCE / name).read_bytes()).hexdigest() for name in watched}
    unchanged = before == after
    report = {'passed': completed and unchanged and not errors and all(item['passed'] for item in fixture.checks),
              'checks': fixture.checks, 'sources_sha256': before, 'source_snapshot_unchanged': unchanged}
    if failure: report['failure'] = failure
    if errors: report['cleanup_errors'] = errors
    path = args.report or Path(tempfile.mkdtemp(prefix='artex-backup-results-')) / 'results.json'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + '\n')
    path.chmod(0o600)
    print(path, flush=True)
    if not report['passed']: raise SystemExit(1)


if __name__ == '__main__': main()

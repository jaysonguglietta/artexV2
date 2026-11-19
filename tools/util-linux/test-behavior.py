#!/usr/bin/env python3
"""Verify real package/tool behavior as the final unprivileged task identity."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

checks = []


def check(name, condition):
    if not condition:
        raise AssertionError(name)
    checks.append({'check': name, 'passed': True})


def run(argv, *, input=None):
    return subprocess.run(argv, input=input, text=True, capture_output=True,
                          timeout=15, check=False)


def main():
    check('The tool regression runs as the actual task UID/GID',
          os.getuid() == 10001 and os.geteuid() == 10001 and os.getgid() == 10001)
    status = dict(line.split(':', 1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
    check('All task capability sets are zero and no-new-privileges is active',
          all(int(status[name].strip(), 16) == 0 for name in ['CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb'])
          and status['NoNewPrivs'].strip() == '1')
    packages = ['bsdutils', 'libblkid1', 'libmount1', 'libsmartcols1', 'libuuid1', 'login', 'mount', 'util-linux']
    for package in packages:
        expected = {'bsdutils': '1:2.42.4-0artex1', 'login': '1:4.16.0-2+really2.42.4-0artex1'}.get(package, '2.42.4-0artex1')
        result = run(['dpkg-query', '-W', '-f=${Version} ${source:Package} ${source:Version}', package])
        check(package + ' has genuine util-linux source/package version 2.42.4-0artex1',
              result.returncode == 0 and result.stdout == expected + ' util-linux 2.42.4-0artex1')
    result = run(['dpkg', '--verify', *packages])
    check('Installed package files match their dpkg integrity manifests',
          result.returncode == 0 and not result.stdout and not result.stderr)
    result = run(['setpriv', '--version'])
    check('The native privilege dropper is util-linux 2.42.4',
          result.returncode == 0 and '2.42.4' in result.stdout)
    # Changing the bounding set requires CAP_SETPCAP, including a repeated drop
    # after confinement. Its already-zero value is asserted above; the live
    # broker fixture separately proves root-to-task removal of every set.
    result = run(['setpriv', '--inh-caps=-all', '--ambient-caps=-all',
                  '--no-new-privs', '/usr/bin/id', '-u'])
    check('An already confined task can execute through unprivileged setpriv options',
          result.returncode == 0 and result.stdout.strip() == '10001')
    result = run(['setpriv', '--reuid=0', '--regid=0', '--clear-groups', '/usr/bin/id', '-u'])
    check('A task cannot use setpriv to regain root', result.returncode != 0 and result.stdout.strip() != '0')
    result = run(['findmnt', '--json', '--target', '/'])
    check('Filesystem inspection remains functional',
          result.returncode == 0 and bool(json.loads(result.stdout)['filesystems']))
    result = run(['nsenter', '--target', '1', '--mount', '--', '/usr/bin/id', '-u'])
    check('A task cannot enter the root supervisor mount namespace',
          result.returncode != 0 and result.stdout.strip() != '0')
    with tempfile.TemporaryDirectory(prefix='artex-util-linux-') as directory:
        path = Path(directory) / 'result'
        result = run(['flock', str(Path(directory) / 'lock'), '/bin/sh', '-ec',
                      'printf scan-output > "$1"', 'sh', str(path)])
        check('Native locking and assessment file output remain functional',
              result.returncode == 0 and path.read_text() == 'scan-output')
    print(json.dumps({'passed': True, 'checks': checks}))


if __name__ == '__main__':
    main()

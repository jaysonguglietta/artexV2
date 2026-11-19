#!/usr/bin/env python3
"""Run compiled mount and nsenter regressions without kernel mount privileges."""
from pathlib import Path
import os
import subprocess
import tempfile


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, timeout=60, **kwargs)


source = Path.cwd()
helpers = Path(__file__).parent
run(['gcc', '-include', 'config.h', '-I.', '-Iinclude', '-Ilibmount/src', '-Ilibblkid/src',
     str(helpers / 'security-regressions.c'), '.libs/libmount.a', '.libs/libblkid.a',
     '.libs/libcommon.a', '-lselinux', '-lsystemd', '-lm', '-o', '/tmp/artex-mount-regressions'])
run(['gcc', '-shared', '-fPIC', str(helpers / 'nsenter-interpose.c'), '-ldl', '-o', '/tmp/artex-nsenter-interpose.so'])
drop = [str(source / 'setpriv'), '--reuid=10001', '--regid=10001', '--clear-groups',
        '--bounding-set=-all', '--inh-caps=-all', '--ambient-caps=-all', '--no-new-privs']
with tempfile.TemporaryDirectory(prefix='artex-util-security-') as directory:
    os.chown(directory, 10001, 10001)
    run([*drop, '/tmp/artex-mount-regressions', directory])
    fixture = Path(directory) / 'cgroup.procs'
    fixture.touch(mode=0o600)
    os.chown(fixture, 10001, 10001)
    environment = os.environ | {'LD_PRELOAD': '/tmp/artex-nsenter-interpose.so', 'ARTEX_CGROUP_FIXTURE': str(fixture)}
    # Set LD_PRELOAD only after setpriv has irreversibly dropped credentials;
    # interposing the privilege-dropper itself would invalidate this proof.
    run([*drop, '/usr/bin/env', 'LD_PRELOAD=' + environment['LD_PRELOAD'],
         'ARTEX_CGROUP_FIXTURE=' + str(fixture), str(source / 'nsenter'),
         '--target', str(os.getpid()), '--mount=/proc/self/ns/mnt', '--join-cgroup',
         '--setuid=10001', '--', '/bin/true'])
    assert fixture.read_text().isdigit(), 'real nsenter did not write its PID to the synthetic cgroup file'
print('nsenter authority regression: 1 passed (no real namespace/cgroup mutation)')

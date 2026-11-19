#!/usr/bin/env python3
"""Prepare pinned upstream util-linux with reviewed Ubuntu package integration."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import tarfile

VERSION = '2.42.4-0artex1'
ARTIFACTS = {
    'util-linux-2.42.4.tar.xz': 'fbd62a100ab7bb8746ba0661255c3c48185b1e9021507c624da01fbc696330ec',
    'util-linux_2.41.3-3ubuntu2.2.debian.tar.xz': 'e8f366168a1483616c1baf06420e341789340dcd2a98b9d8423f076d6cffa043',
}
RETAINED = [
    'debian/man-getopt-examples.patch',
    'debian/verbose-tests.patch',
    'debian/hardlink-tests-known-failed.patch',
    'artex/lsfd-usrbin-2.42.patch',
    'debian/sensible-pager.patch',
    'debian/login-support-LOG_OK_LOGINS.patch',
    'debian/login-do-not-print-motd-done-by-PAM.patch',
    'debian/login-turn-off-btmp-utmp-lastlog-writing.patch',
    'debian/salsa-ci-skip-tests.patch',
    'artex/sulogin-static-sh-2.42.patch',
    'ubuntu/sulogin-lockedpwd.patch',
    'ubuntu/su-pty-drop-caps.patch',
    'tests-fincore-force-the-use-of-GNU-dd-over-uutils.patch',
]


# Only additive ABI nodes/functions from the pinned upstream 2.42 symbol maps.
# Keep every old template entry and Ubuntu's dh_makeshlibs -c4; new or removed
# symbols outside this reviewed set still fail the build.
SYMBOL_ADDITIONS = {
    'libmount1.symbols': ('MOUNT_2_42', [
        'mnt_context_enable_beneath', 'mnt_context_enable_exclusive',
        'mnt_context_is_beneath', 'mnt_context_is_exclusive',
        'mnt_fs_is_attached', 'mnt_fs_is_detached', 'mnt_fs_is_moved',
        'mnt_monitor_enable_fanotify', 'mnt_monitor_enable_mountinfo',
        'mnt_monitor_event_next_fs',
    ]),
    'libsmartcols1.symbols': ('SMARTCOLS_2.42', [
        'scols_column_get_annotation', 'scols_column_get_headercolor',
        'scols_column_refer_annotation', 'scols_column_set_headercolor',
        'scols_filter_has_holder',
    ]),
    'libfdisk1.symbols': ('FDISK_2_42', ['fdisk_is_collision_area']),
}


def validate_proofs(source: Path, proofs: list) -> None:
    if not proofs:
        raise ValueError('removed/security patch has no explicit upstream proof')
    for proof in proofs:
        content = (source / proof['path']).read_bytes()
        if hashlib.sha256(content).hexdigest() != proof['sha256']:
            raise ValueError('upstream source proof mismatch: ' + proof['path'])
        text = content.decode()
        if any(value not in text for value in proof['required_strings']) or any(value in text for value in proof['forbidden_strings']):
            raise ValueError('upstream source semantic marker mismatch')


def validate_review(source: Path, review: dict) -> None:
    if review['source_package_version'] != VERSION or review['upstream_version'] != '2.42.4':
        raise ValueError('unexpected reviewed source version')
    if {name: entry['sha256'] for name, entry in review['artifacts'].items()} != ARTIFACTS:
        raise ValueError('review/source artifact pin mismatch')
    patches = source / 'debian/patches'
    series = [line.strip() for line in (patches / 'series').read_text().splitlines()
              if line.strip() and not line.startswith('#')]
    dispositions = review['patch_dispositions']
    if series != review['expected_ubuntu_patch_series'] or [entry['path'] for entry in dispositions] != series:
        raise ValueError('unreviewed Ubuntu patch series change')
    retained = []
    for entry in dispositions:
        patch = patches / entry['path']
        if hashlib.sha256(patch.read_bytes()).hexdigest() != entry['sha256']:
            raise ValueError('Ubuntu patch checksum mismatch: ' + entry['path'])
        kind = entry['disposition']
        if kind == 'retained':
            if entry['retained_path'] != entry['path']:
                raise ValueError('retained patch was renamed without review')
            retained.append(entry['retained_path'])
        elif kind == 'refreshed':
            replacement = Path(__file__).parent / Path(entry['retained_path']).name
            if hashlib.sha256(replacement.read_bytes()).hexdigest() != entry['replacement_sha256']:
                raise ValueError('refreshed patch checksum mismatch')
            retained.append(entry['retained_path'])
        elif kind == 'superseded':
            if not entry['proof_files'] or not entry['upstream_commit']:
                raise ValueError('removed patch has no explicit upstream proof')
            validate_proofs(source, entry['proof_files'])
        else:
            raise ValueError('unknown patch disposition')
    if set(review['security_fixes']) != {'CVE-2026-76642', 'CVE-2026-78408', 'CVE-2026-78410'}:
        raise ValueError('security fix review is incomplete')
    for fix in review['security_fixes'].values():
        validate_proofs(source, fix['proof_files'])
    if retained != RETAINED or retained != review['retained_patch_series']:
        raise ValueError('retained patch order/review mismatch')
    additions = {name: (entry['node'], entry['functions']) for name, entry in review['abi']['additions'].items()}
    if additions != SYMBOL_ADDITIONS:
        raise ValueError('ABI additions do not match the reviewed allowlist')


def prepare(artifacts: Path, work: Path) -> Path:
    for filename, expected in ARTIFACTS.items():
        if hashlib.sha256((artifacts / filename).read_bytes()).hexdigest() != expected:
            raise ValueError('source checksum mismatch: ' + filename)
    work.mkdir(parents=True, exist_ok=True)
    source = work / 'util-linux-2.42.4'
    if source.exists():
        raise ValueError('refusing to reuse an existing source tree')
    with tarfile.open(artifacts / 'util-linux-2.42.4.tar.xz') as archive:
        archive.extractall(work, filter='data')
    with tarfile.open(artifacts / 'util-linux_2.41.3-3ubuntu2.2.debian.tar.xz') as archive:
        archive.extractall(source, filter='data')
    review = json.loads((Path(__file__).parent / 'source.json').read_text())
    validate_review(source, review)
    patches = source / 'debian/patches'
    (patches / 'artex').mkdir()
    for name in ['lsfd-usrbin-2.42.patch', 'sulogin-static-sh-2.42.patch']:
        shutil.copyfile(Path(__file__).parent / name, patches / 'artex' / name)
    (patches / 'series').write_text('\n'.join(RETAINED) + '\n')
    for filename, (node, names) in SYMBOL_ADDITIONS.items():
        template = source / 'debian' / filename
        current = template.read_text()
        additions = [f' {node}@{node} 2.42']
        for name in names:
            additions.append(f' {name}@{node} 2.42')
        template.write_text(current + '\n'.join(additions) + '\n')
    changelog = source / 'debian/changelog'
    original = changelog.read_text()
    if not original.startswith('util-linux (2.41.3-3ubuntu2.2)'):
        raise ValueError('unexpected Ubuntu packaging revision')
    changelog.write_text(f'''util-linux ({VERSION}) resolute; urgency=high

  * Build upstream 2.42.4, including CVE-2026-76642, CVE-2026-78408 and
    CVE-2026-78410 fixes and their descriptor-close/legacy-mount follow-ups.
  * Retain Ubuntu configuration, hardening and distribution-specific patches.
    Refresh only lsfd install-path and sulogin static-shell patch context.
    Remove redundant upstream/CVE patches already superseded by this release.
  * Keep the upstream source identity and genuine Debian binary metadata.

 -- Artex maintainers <security@artex.invalid>  Thu, 19 Nov 2026 00:00:00 +0000

''' + original)
    shutil.copyfile(artifacts / 'util-linux-2.42.4.tar.xz', work / 'util-linux_2.42.4.orig.tar.xz')
    return source


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('artifacts', type=Path)
    parser.add_argument('work', type=Path)
    args = parser.parse_args()
    print(prepare(args.artifacts, args.work))

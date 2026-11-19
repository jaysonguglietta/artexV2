# Native util-linux security upgrade

The worker builds **upstream util-linux2.42.4** using Ubuntu26.04's reviewed
packaging, configuration and distribution patches. The local source-package
version is **`2.42.4-0artex1`**. This is an ARTEX derivative, not an official Ubuntu
security release or a version-only relabel of older binaries.

The worker replaces eight installed native packages and their actual files:

| Packages | Binary version |
|---|---|
| `libblkid1`, `libmount1`, `libsmartcols1`, `libuuid1`, `mount`, `util-linux` | `2.42.4-0artex1` |
| `bsdutils` | `1:2.42.4-0artex1` |
| `login` | `1:4.16.0-2+really2.42.4-0artex1` |

The epochs and `login` compatibility prefix come from Ubuntu's existing packaging
rules. Every binary still identifies its real source as `util-linux`, source
version `2.42.4-0artex1`; the prefix does not claim a separate shadow release was
patched. Shared libraries are replaced through native packages with their normal
ABI names. Build dependencies, unused development packages and source trees stay
out of the final worker.

## Source integrity and provenance

[source.json](source.json) records exact URLs/hashes, signature evidence, the full
original patch series and every retained/refreshed/superseded patch decision.

| Input | SHA256 |
|---|---|
| [Official util-linux2.42.4 archive](https://cdn.kernel.org/pub/linux/utils/util-linux/v2.42/util-linux-2.42.4.tar.xz) | `fbd62a100ab7bb8746ba0661255c3c48185b1e9021507c624da01fbc696330ec` |
| [Ubuntu2.41.3-3ubuntu2.2 packaging archive](https://azure.archive.ubuntu.com/ubuntu/pool/main/u/util-linux/util-linux_2.41.3-3ubuntu2.2.debian.tar.xz) | `e8f366168a1483616c1baf06420e341789340dcd2a98b9d8423f076d6cffa043` |
| [Detached upstream tar signature](https://cdn.kernel.org/pub/linux/utils/util-linux/v2.42/util-linux-2.42.4.tar.sign) | `d3b4e930a38c3b4512761708d0d226a55a1420995e3996c1efca0910d7effd3e` |

The initial source review verified the detached signature over the **uncompressed
tar**, obtaining `GOODSIG` and `VALIDSIG` for full fingerprint
`B0C64D14301CC6EFAEDF60E4E4B71D5EEC39C284`. That complete fingerprint is also
independently published as Karel Zak's release key in official Arch maintainer
records from [2018](https://lists.archlinux.org/pipermail/arch-commits/2018-February/462844.html)
and [2019](https://lists.archlinux.org/pipermail/arch-commits/2019-November/668636.html).
The Ubuntu keyserver supplied the public-key bytes; a keyserver lookup alone
does not authenticate an identity. This is an explicit, independently pinned key
association, not a claim that ARTEX established a new upstream web of trust.

BuildKit and the preparation helper enforce the reviewed SHA256 bytes before
extraction. Builds do **not** redownload or independently verify OpenPGP keys on
every run. Ubuntu's packaging-tar digest was cross-checked against its official
HTTPS source descriptor; its developer signature was not independently verified.
APT verifies signed Ubuntu metadata for build dependencies. These are distinct
checks and should not be described interchangeably.

## Distribution patch accounting

The upstream [2.42.3 security release](https://www.kernel.org/pub/linux/utils/util-linux/v2.42/v2.42.3-ReleaseNotes)
addresses failed-helper post-mount hooks (CVE-2026-76642), namespace descriptor
leaks (CVE-2026-78408), and mount-source races (CVE-2026-78410). The selected2.42.4
release additionally closes the cgroup descriptor explicitly and hardens legacy
mount source/target handling. `source.json` records those current fixes separately
from the distribution-patch accounting below.

The pinned Ubuntu package has **26** ordered patches. The derivative retains
**11 unchanged**, refreshes **2** for equivalent source context, and supersedes
**13** with corresponding upstream fixes. Each original patch has a recorded
SHA256. Superseded entries include the upstream commit and exact proof-file
hashes/guards from the pinned release. A source-version comparison alone is not
the proof.

The two refreshed patches preserve `/usr/bin/lsfd` installation and Ubuntu's
`/bin/static-sh` emergency-shell fallback. Ubuntu's locked-password and `su` PTY
capability-drop patches remain. PAM/login behavior, hardening options and
distribution installation layout remain governed by the retained Ubuntu rules.

| Superseded patch group | Preserved upstream behavior |
|---|---|
| Five `upstream/` patches | GSM0710 MTU/MRU setting, ZFS pool naming, namespace owner handling, eventfd synchronization and user-namespace bind persistence. |
| CVE-2026-13595 | Stable, individually allocated partition objects behind a pointer array. |
| CVE-2026-27456 | No-follow loop-device backing-file handling. |
| CVE-2026-3184 | Original remote FQDN passed to PAM host checks. |
| CVE-2026-53612 | Descriptor-based ownership/mode changes. |
| CVE-2026-53613 and prerequisite | All-component symlink rejection and pinned mount targets. |
| CVE-2026-53614 | Safe environment access and restricted legacy multi-step mount refusal. |
| CVE-2026-53615 | Bounds for EBR partition data and links. |

Ubuntu's `--disable-libmount-mountfd-support` setting is retained. The review
therefore checks the **legacy** mount path: pinned target descriptors, restricted
multi-step refusal and safe environment handling remain present without relying
solely on new-mount-only code. The upstream
[2.42.4 release notes](https://www.kernel.org/pub/linux/utils/util-linux/v2.42/v2.42.4-ReleaseNotes)
also describe legacy mount hardening and a cgroup-descriptor close follow-up;
installing the earlier initial fix alone would miss those follow-ups.

## ABI and installation

The original Ubuntu symbol templates remain intact. Only the explicitly reviewed
additive2.42 nodes/functions in `source.json` are appended for libmount,
libsmartcols and libfdisk. Ubuntu's **`dh_makeshlibs -- -c4` remains enabled**;
unlisted additions or removed existing symbols must fail the build. This checks
published symbol compatibility, not every possible C API semantic behavior.

The normal Debian build generates genuine architecture-specific packages plus
`.dsc`, original/packaging source archives, `.buildinfo` and `.changes` under
the builder's `/out/source`. Retain those generated source/provenance artifacts
when distributing this derivative; their existence in a local builder is not a
claim they have already been published. The selected packages install only after
the worker's final Ubuntu apt operation. Normal package inventory, configuration,
licensing, manuals and integrity manifests remain visible; old packages are not
hidden by copying only a new `setpriv` over them.

The source and packaging archives preserve upstream/distribution license notices.
The runtime installer preserves package documentation/manifests despite Ubuntu's
minimal-image path exclusions and verifies the installed source/version/file
identities. Later apt operations need a fresh inventory check so that the reviewed
family cannot silently be replaced or partly mixed with an older build.

## Validation and maintenance

```sh
docker build --platform linux/arm64 --target util-linux-security -f Dockerfile.source .
docker build --platform linux/amd64 --target util-linux-security -f Dockerfile.source .
python3 scripts/test-worker-tools.py --image artex-local:worker
python3 scripts/test-workers.py
```

[test-behavior.py](test-behavior.py) is intended for the final task UID10001 with
zero capabilities, no-new-privileges, a read-only root and no network. It checks
all eight package identities and file manifests, real `setpriv` execution and
root-regain rejection, namespace-entry rejection, filesystem inspection and file
locking. Full worker tests must also exercise the broker's **root-to-task**
credential drop and watchdog lifecycle; success only from an already confined
task is not proof of that separate transition.

Inspect actual native build/test logs for architecture coverage and hardware/kernel
skips. Do not treat a draft build failure as an exploitable vulnerability or an
ARM64 pass as AMD64 proof. Final image reports must retain unresolved findings;
Ubuntu-vendor-unfixed status is distinct from a reviewed upstream source fix.
Prefer official updated distribution packages when available, and re-review
source signatures, patch accounting, ABI changes and complete runtime behavior
before changing these pins.

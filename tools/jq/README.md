# Native jq security upgrade

ARTEX builds **unmodified upstream jq1.8.2** as native Ubuntu26.04 packages
`jq` and `libjq1`, each version **`1.8.2-0artex1`**. The local revision describes
ARTEX packaging, not an upstream fork or a fabricated Ubuntu security release.
The source package remains `jq`; its actual version, installed files and shared
library remain visible to dpkg and vulnerability inventories.

| Input | Exact identity |
|---|---|
| Official release | [jq1.8.2](https://github.com/jqlang/jq/releases/tag/jq-1.8.2) |
| Source archive | [jq-1.8.2.tar.gz](https://github.com/jqlang/jq/releases/download/jq-1.8.2/jq-1.8.2.tar.gz) |
| Archive SHA256 | `71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed095abd315e26e6e1193af3af` |
| Native package version | `1.8.2-0artex1`, architecture-specific `amd64` or `arm64` |
| Upstream license | [Complete COPYING notices](https://github.com/jqlang/jq/blob/jq-1.8.2/COPYING) |

The upstream [release notes](https://github.com/jqlang/jq/blob/jq-1.8.2/NEWS.md)
identify fixes for all five Medium CVEs in ARTEX's prior jq1.8.1 inventory:

| CVE | Upstream fix |
|---|---|
| CVE-2026-41256 | Reject NUL truncation in program files; commit `5a015deae35d19e3ebbc65db6c157a80e76df738`. |
| CVE-2026-41257 | Guard integer overflow in stack allocation; commit `01b3cded76daacbfddb7f8763700b0803bcb5c6f`. |
| CVE-2026-43895 | Reject NUL bytes in module import paths; commit `9d223f153c3632a207fa071caaa6292da33ae361`. |
| CVE-2026-43896 | Bound recursive object merging; commit `532ccea6080ed6758f39fe9f6208a44b665023d2`. |
| CVE-2026-44777 | Detect circular module imports; commit `f58787c41835d9b17795730cb04925fdba25c71c`. |

The separate Low [CVE-2025-9403](https://ubuntu.com/security/CVE-2025-9403)
concerns the local test runner. This package does not claim that advisory is
fixed merely because the five named Medium fixes are present. Preserve any
remaining raw scanner finding and document its precise disposition. Ubuntu's
advisory feed can also continue reporting distribution-unfixed findings against
an independently upgraded source package; a local source build is not an Ubuntu
published fixed-version claim. Do not erase package identity or suppress findings
to manufacture a clean report.

## Package construction

[build-debs.sh](build-debs.sh) verifies the pinned tar hash before extraction or
compilation. The architecture-specific Ubuntu build stage installs compiler and
development dependencies separately from the worker. It builds shared libjq
with Debian hardening flags and system `libonig5`, refuses bundled-regex fallback,
runs upstream `make check` with networking disabled, and creates real `.deb`
packages using `dpkg-deb`. Shared-library dependencies are derived using
`dpkg-shlibdeps`; jq depends on the exact newly built libjq package version.
Both native builds also enforce SONAME `libjq.so.1` and the checksum-pinned
[181 exported-symbol baseline](abi-symbols.txt), measured identical between
Ubuntu `1.8.1-4ubuntu2` and the new ARM64 build. No missing or unexpected `jq_`/`jv_`
exports are accepted. This verifies exported names; it does not prove every
calling convention, structure layout or semantic compatibility property.

The final worker installs both packages after its last apt/browser operation.
dpkg replaces the old owned binary, shared library and version records. There is
no `/usr/local/bin/jq` overlay leaving an older callable libjq behind. Both
packages retain the complete upstream license, author/release information and
the exact source manifest. The command also retains the upstream prebuilt manual.
Installed-file checksums and the library cache are updated normally.
The installer explicitly includes only these two packages' documentation and jq's
manual, overriding Ubuntu's minimal-image documentation exclusions for those
paths. This preserves the source manifest and keeps dpkg's file inventory honest;
the global exclusions for unrelated packages remain intact.

This builds runtime packages, not a developer SDK. Headers and static archives
are deliberately absent from the worker. Ubuntu's separately installed regex
library keeps its own package inventory and licensing.

## Validation

```sh
docker build --platform linux/arm64 --target jq-security -f Dockerfile.source .
docker build --platform linux/amd64 --target jq-security -f Dockerfile.source .
# Exercise the actual worker through its network-free unprivileged fixture:
python3 scripts/test-worker-tools.py --image artex-local:worker
```

[test-behavior.py](test-behavior.py) runs 17 installed-package, shared-library,
license, normal-CLI and hostile-input checks. It verifies source/package identity,
dpkg ownership/integrity, system regex linkage, JSON construction, quoted
arguments, multiple scan records, raw NUL preservation, invalid JSON/program/module
NUL handling, circular imports, excessive paths/merges, runtime type errors,
regex capture and exit-status semantics. Each child has a finite deadline and
crashes are failures. Scratch is temporary; no network or credentials are used.

The depth cases follow actual upstream boundaries: 10000 levels remain permitted,
while 10001 must produce a handled error. The type check invokes a real builtin
runtime error, rather than an invalid-arity compiler error. The suite does not
allocate multi-gigabyte strings to reproduce integer-overflow exploits and is not
a substitute for upstream tests, architecture builds, final image scanning or
the complete worker isolation suite. Record actual results separately; ARM64
success alone does not prove AMD64 behavior.

## Maintenance

Prefer official updated Ubuntu packages once available. Updating this source pin
requires reviewing upstream advisories/license changes, rebuilding both native
packages, running upstream and installed tests on both supported architectures,
checking the final worker's inventory and rerunning worker isolation checks.
Keep actual source/version identities honest and preserve unresolved findings.

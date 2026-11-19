# Explicit Python 3.14 pip security fork

The worker uses **pip `26.3+artex.1`**, a deliberately modified build of official
pip `26.3`. This is not an upstream release or a version-only vulnerability fix.
The fork removes the complete unused vendored `pkg_resources` subset, its legacy
metadata backend, vendor declaration and corresponding license declaration/files.
The retained upstream CycloneDX file is corrected only after the actual code is
removed: the exact `pkg:pypi/setuptools@70.3.0` component and its dependency edges
are deleted, the document version increments, and pip's root metadata identifies
the honest fork version. All other components/versions/relationships remain.
It does not remove a separately installed full `setuptools` distribution or claim
that full setuptools was present inside pip.

| Source or output | Exact identity |
|---|---|
| Official source release | [pip 26.3 on PyPI](https://pypi.org/project/pip/26.3/) |
| Verified source wheel | [pip-26.3-py3-none-any.whl](https://files.pythonhosted.org/packages/07/25/ee8c38ae72d66b844ce8eb9711ba905dba21ca1a8a0c960057c41cd23337/pip-26.3-py3-none-any.whl) |
| Source wheel SHA256 | `f3b60b20ffaadd4415ed7957212a38ac63fa4c5fa375bd0f4a797777940fad13` |
| Fork version / interpreter metadata | `26.3+artex.1` / `Requires-Python: >=3.14` |
| Pinned source runtime | `python:3.14.10-slim-trixie@sha256:9aaa474097d094040649f2162a627c0049b408f52138d980916788d80f04802f` |

The actual upstream [metadata selector](https://github.com/pypa/pip/blob/26.3/src/pip/_internal/metadata/__init__.py)
unconditionally chooses `importlib.metadata` on Python 3.14 and later, before
consulting the legacy `_PIP_USE_IMPORTLIB_METADATA` environment switch. The removed
backend therefore cannot be selected on the provided Python 3.14.10 runtime. Older
interpreters are explicitly outside this fork's supported metadata requirement.
This removes no selectable metadata backend on that supported runtime; it is not
a claim of complete compatibility with every third-party project or older Python.

Upgrading from the source runtime's older bundled pip to official 26.3 first
updates real vendored dependencies. Its [vendor inventory](https://github.com/pypa/pip/blob/26.3/src/pip/_vendor/vendor.txt)
contains msgpack 1.2.4 and urllib3 2.8.0. The separate legacy subset is attributed
upstream to setuptools 70.3.0. Package-level advisory attribution to that small
subset is not proof that all setuptools features existed or were callable, but
leaving the obsolete component is unnecessary. The fork removes its actual bytes
and inventory rather than suppressing advisories or pretending upstream released
a different fixed setuptools version. Final image scanning remains required.

## Build behavior

[patch-pip.py](patch-pip.py) accepts only the exact source wheel SHA256. It bounds
and rejects unsafe ZIP entries, verifies the expected upstream Python 3.14 selector
and component identity, removes the component, and changes pip's module/distribution
identity honestly. It preserves unrelated package code and remaining upstream
licenses, removes the obsolete `License-File` reference, corrects the retained
SBOM to match the actual removal, and regenerates wheel
`RECORD` hashes/sizes over every resulting file. Output is deterministic and an
existing destination is refused.

The source [CPython ensurepip bundle](https://github.com/python/cpython/blob/v3.14.10/Lib/ensurepip/__init__.py)
is replaced at build time with this patched wheel and its version declaration.
That matters because installing only the global pip would leave `python -m venv`
or `python -m ensurepip` able to restore the old vulnerable bundle. The worker
build then installs the patched wheel with no index or dependency resolution.
There is no downloaded installer script, live-environment migration or repository
copy of an installed dependency tree. Parent pip/Python licenses remain intact.

Run patching only in the disposable reviewed image build. Do not use this helper
to rewrite an unrelated live Python installation: unexpected source/ensurepip
versions deliberately fail rather than guessing how to patch them.

## Validation

```sh
python3 -I scripts/test-pip-fork.py
# On the actual built Python 3.14 runtime, with writable executable private scratch:
python3 tools/python/test-behavior.py
```

Three offline integrity/security regressions cover exact honest identity,
component/license removal, exact SBOM component/edge correction with unrelated
inventory preserved, complete deterministic `RECORD`, checksum/ZIP/
upstream-proof tampering, and explicit ensurepip bundle replacement. Synthetic
wheel fixtures are local test inputs; they are not substitute downloaded sources.

The installed behavior suite uses two disposable virtual environments and an
owned pure-Python wheel. It verifies:

- Installed module/distribution identity, absent component/backend files, retained
  license inventory and installed `RECORD` hashes. The installed SBOM is compared
  with a canonical digest derived from the verified upstream wheel after this
  one removal; every retained vendored package must still have matching declared
  versions and importable code. The old SBOM entry is not hidden while its code
  remains installed.
- CLI version/list/check and supported metadata selection even when the old
  environment switch requests the unavailable backend.
- Default `venv` creation and explicit `ensurepip` bootstrapping the actual patched
  wheel offline, rather than merely reporting the expected version.
- Hash rejection without partial installation, successful hash-verified offline
  install, module/metadata imports and direct console-script execution.
- `inspect`, local `download`, wheel-copy and uninstall behavior, ending with a
  clean dependency check and no leftover fixture command/package.

[Fixture evidence](../../security/pip-fork-validation.json) records ten passing
checks on Linux ARM64/Python 3.14.10, using the pinned official source runtime.
The container had network disabled, UID/GID 10001, no capabilities, no Docker
socket and a read-only root. Its bounded private tmpfs allowed execution because
venv console scripts are executed directly; a `noexec` fixture would test a
filesystem prohibition rather than pip behavior. All pip package operations also
explicitly disabled index access. Temporary test files are removed on completion.

The suite does not run the full upstream pip suite, source-distribution build
backends, network indexes, every platform or every third-party package. pip's
internal metadata API is exercised for this exact release and is not asserted to
be a stable public integration contract. Copying the runtime into the Ubuntu worker
also needs the actual worker's Python/SSL/tool behavior checks and final image
inventory; this base-runtime fixture alone does not prove that integration.

## Maintenance

Keep this fork small and visible. For a new upstream release, verify official
source/integrity, interpreter selection and component contents again. Preserve
licenses and rebuild `RECORD`/ensurepip together, run both integrity and actual
installed CLI regressions, and scan the final worker. Prefer removing the fork
when upstream no longer bundles the obsolete component. Do not relabel unchanged
vulnerable code, hide SBOM records, introduce broad exceptions, or restore an old
ensurepip wheel over the reviewed bundle.

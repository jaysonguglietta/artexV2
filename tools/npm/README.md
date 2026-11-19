# Explicit npm security fork

This directory contains source manifests and the complete lock for
**npm `12.2.3+artex.1`**. It is an explicitly modified build of npm `12.2.3`, not a
claim that upstream published a patched npm release. Upstream code and license
come from the verified official npm archive; the fork changes its package
identity, pins `glob` to `14.0.6` and `minimatch` to `11.0.9`, and applies those
overrides to every nested dependency. It remains private to prevent accidental
publication as the upstream `npm` package.

| Source | Identity |
| --- | --- |
| Official upstream archive | `https://registry.npmjs.org/npm/-/npm-12.2.3.tgz` |
| Archive SHA256 | `fa0711dd513fa1386bbad3962a292cb813461cb57dedf07d6eb7efe76dffe897` |
| Fork version | `12.2.3+artex.1` |
| Node engine inherited from npm | `^22.22.2 || ^24.15.0 || >=26.0.0` |
| Locked dependency entries | 147 |

The official active advisories describe glob cancellation continuing background
filesystem traversal ([CVE-2026-118773 / GHSA-3g3m-2fjr-6pw4](https://github.com/advisories/GHSA-3g3m-2fjr-6pw4))
and minimatch preprocessing amplification
([CVE-2026-118764 / GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249)).
Their listed patched versions are glob `14.0.2` and minimatch `11.0.1`; this fork
pins later reviewed fixes. No advisory is suppressed or relabeled as withdrawn.
Patching the actual dependency tree is required because updating only npm's
visible version leaves its bundled vulnerable packages in the image.

## Build and validate

The worker build extracts the checksum-verified upstream archive into a fresh
tree, overlays `package.json` and `package-lock.json`, and uses the builder's
separate bootstrap npm to run `npm ci --omit=dev --ignore-scripts`. Clean install
replaces the upstream bundled `node_modules`; the resulting fork tree is copied
into the runtime with its real modified version. Bootstrap npm is a build tool,
not the runtime fork. Upstream license notices remain in the extracted tree.
The repository carries no downloaded archive or installed dependency tree.

```sh
python3 -I scripts/test-npm-fork.py
# Against an installed fork on a supported Node version:
node tools/npm/test-behavior.mjs /path/to/patched/npm
```

The static check rejects unpinned/nested vulnerable copies, missing integrity,
untrusted registry archives and false upstream/fork identities. The behavior
check uses private temporary fixtures and no publish operation. Its CLI cases
exercise local `install`, clean `ci`, package `pack` inclusion/exclusion rules,
`exec`, and workspace glob discovery. It also tests the public glob/minimatch APIs,
verifies cancellation stops recursive directory reads, and runs an adversarial
minimatch pattern under a bounded child-process timeout. Installed-tree inspection
checks for overlooked bundled copies. Temporary fixture files are removed on exit.

The Docker validation fixture runs these behavior checks with network access
disabled, UID/GID 10001, no capabilities and a read-only root. Its private scratch
tmpfs explicitly permits execution: npm/npx dispatch package binaries directly,
so a `noexec` cache would reject that workflow regardless of package versions.
This concerns only the already untrusted worker's scratch filesystem; it does
not authorize host execution or broaden network access.
A separate fresh `npm audit --omit=dev --audit-level=low --json` checks
the entire installed production graph against the current registry. That result
and final runtime image scans must be recorded by the release validation; a lock
check alone is not a live advisory scan or a full upstream npm test suite.

The [redacted fixture evidence](../../security/npm-fork-validation.json) records
ten passing behavior/security checks on Node `24.22.3`/Linux arm64 and a fresh
audit with zero findings at every severity. It authenticates the manifest, lock
and test-script hashes. This validates the npm fork, not the complete final
worker image or its network controls.

## Compatibility and maintenance

This retains npm 12 behavior. In particular, `npm pack --json` returns an object
keyed by package name rather than npm 11's array. npm 12 also changes lifecycle
approval, remote/git dependency defaults and some CLI flags; see the official
[npm 12 release notes](https://github.com/npm/cli/releases/tag/v12.0.0).
The dependency override must pass actual CLI regressions rather than relying on
major-version numbers being compatible. It is not a promise to preserve every
npm 11 command or third-party script's old output assumptions.

Keep this fork small. When an upstream npm archive bundles fixed dependencies,
verify its source/integrity and advisory inventory, run the behavior/image checks,
and deliberately remove this fork if no longer necessary. Do not edit package
version strings to conceal vulnerable code, copy an old dependency tree over the
patched one, or add a vulnerability exception instead of fixing the tree.

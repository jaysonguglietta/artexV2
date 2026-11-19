# Worker jq and util-linux security packages

## Status and evidence scope

This follow-up is **in progress** against the source published at `3062e9c…`.
The operator authorized upgrading jq and applying a maintainable util-linux security upgrade
while preserving assessment tools, both native architectures and worker isolation.
Final package integration, versions, runtime tests, image findings and publication
checks remain pending. This guide does not claim that the new worker is already
built, passes its gate or has any particular new severity count.

The earlier `375b2c3…` checkpoint recorded 85 Medium/9 Low worker records per
architecture and 188 exact risk reviews. Those are historical observations, not
results for the new package build. [Validation](VALIDATION.md) will record the
fresh source/image/DB identities and measured results; [image policy](IMAGE-SECURITY.md)
defines gate behavior.

## A missing Ubuntu fix is not a missing upstream fix

The prior Trivy Ubuntu report had no `FixedVersion` for the selected records.
That means its Ubuntu advisory data did not identify a fixed **Ubuntu package**
at that DB snapshot. It does not establish that upstream has no release, patch
or safe backport. Distribution package availability, upstream patch availability
and applicability to the exact installed code must be reviewed separately.

For example, the official [jq 1.8.2 release](https://github.com/jqlang/jq/releases/tag/jq-1.8.2)
explicitly includes fixes for the baseline Medium records CVE-2026-41256,
CVE-2026-41257, CVE-2026-43895, CVE-2026-43896 and CVE-2026-44777. An Ubuntu
feed with an empty fixed-version field is not evidence that those upstream fixes
do not exist. This statement does not classify every jq advisory or certify the
new package; exact code, build and final scan evidence are still required.

The separate baseline Low [CVE-2025-9403](https://ubuntu.com/security/CVE-2025-9403)
needs a distinct disposition. Ubuntu currently marks 26.04 vulnerable while giving
this local test-suite assertion negligible priority. That does not prove jq 1.8.2
fixed or eliminated it. Preserve any raw scanner record and review the exact
installed test behavior rather than borrowing the five Medium fixes' conclusion.

Containment and an expiring risk review remain relevant for residual exposure,
but they do not replace an available, reviewed engineering fix. Keep every raw
finding visible and distinguish an applied fix from accepted risk or a disputed
scanner attribution.

## Native packages must describe the code that actually runs

The visible jq work pins official upstream **1.8.2** and proposes actual native
`jq` and `libjq1` packages at **`1.8.2-0artex1`**. See the
[source manifest](../tools/jq/source.json) for the exact archive URL/SHA256 and the
[package builder](../tools/jq/build-debs.sh). These are ARTEX package revisions,
not official Ubuntu packages. Their final installation and validation are pending.

Both the executable and its loaded shared library must be replaced together;
putting a new binary in `/usr/local/bin` while an old library/package record
remains is incomplete. Dependencies, dynamic linkage, package ownership,
architecture, licenses and installed file hashes must agree with the actual
runtime. The staged jq builder uses the inventoried system regex library rather
than hiding another bundled copy. Source/manifests and package provenance belong
in source control; downloaded archives, installed dependencies and built `.deb`
files do not belong in the publication copy.

The proposed util-linux build now uses upstream **2.42.4** with the existing
Ubuntu **2.41.3-3ubuntu2.2 packaging/configuration** as its integration basis.
Its honest intended native package version is **`2.42.4-0artex1`**, preserving
required binary-package epochs and login variants. This is a real upstream code
upgrade in ARTEX-maintained Ubuntu-native packages, not an official Ubuntu
security release and not relabeled 2.41.3 bytes.

The original 2.41 backport plan would require a substantial set of active legacy
mount-path fixes and prerequisites beyond the released stable branch. Rather than
publish a partial cherry-pick, the revised plan uses the complete upstream source,
retains applicable reviewed Ubuntu-specific patches/configuration, and requires
strict `dh_makeshlibs -c4` symbol/SONAME checks against the existing ABI contract.
Those checks are necessary build guards, not proof of identical runtime semantics.
The review still targets CVE-2026-76642, CVE-2026-78408 and CVE-2026-78410, including
descriptor-lifetime and legacy/new mount paths. Source/patch integration, complete
native package builds, ABI results and live tool/isolation checks are **pending**.

All affected installed binaries/libraries and package records need coherent
replacement. Any remaining scanner attribution must be reconciled with exact
source proof and a fresh DB, not concealed by editing version strings or deleting
SBOM records while retaining code.

## Raw alerts, source fixes and remaining risk

A distribution advisory feed can continue attributing a CVE to an honest custom
package after its upstream code has been fixed. Do not predict that a jq or
util-linux version change will remove an alert. Final evidence must distinguish:

- **Raw scanner records:** the unfiltered package/advisory matches and severities
  reported for exact images, architectures and DB identity.
- **Source-remediated records:** only fixes supported by pinned source/patch
  identity and completed relevant build/runtime tests. This is separate from
  whether Ubuntu's feed recognizes the ARTEX package version.
- **Actually unpatched residual exposure:** reviewed against the exact remaining
  code, with concrete containment and a remediation/expiry plan.
- **Exact accepted scanner records:** any gate review still required for a raw
  match, bound to the actual new package version, CVE, severity and architecture.
  A source-fix annotation does not itself grant a gate bypass.

Keep source manifests, tests and raw reports together. Do not use suppression,
VEX filters, false package/source identities or a fabricated upstream version to
make these categories collapse into a zero-finding result. Update exact reviews
only from actual new reports, preserving their approved expiry. Final measured
counts and source-fix classifications remain pending until verification completes.

## Completion criteria

- Pin original source and every patch to immutable identities/checksums, retain
  attribution/licenses, and verify the patch applies only to expected source.
- Build and inspect architecture-correct packages on native amd64 and arm64;
  verify loaded libraries and package inventory match actual installed files.
- Exercise jq JSON/filter/module behavior and the relevant adversarial fixes;
  exercise util-linux tools used by worker startup and assessment workflows.
- Repeat the expanded **54-check worker boundary** suite, including an actual
  unprivileged `nmap -sT -Pn -n` connection scan of the authorized synthetic port.
  The **eight tool groups** include jq17, util-linux17 and real Chromium behavior;
  these are required new integration results, not passes inferred from earlier
  53-check or six/seven-group checkpoints. Repeat privilege, egress and
  cancellation/watchdog checks. Do not relax capabilities, AppArmor, no-new-privileges or target rules
  to compensate for an incompatible package.
- Scan all exact final component images on both architectures with fresh DB
  identity. Record raw findings, proven fixes and residual reviewed risks without
  predicting how many alerts a custom package will remove.
- Refresh stale exact risk reviews from actual results, repeat source/credential
  and archive-manifest checks, and preserve the reviewed release identity policy.

Recorded passes will be added only after the corresponding checks complete.
A successful build or a changed package version alone does not fulfill these
criteria or authorize a formal release.

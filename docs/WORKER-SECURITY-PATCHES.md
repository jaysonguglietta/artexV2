# Worker jq and util-linux security packages

## Status and evidence scope

The authorized follow-up is implemented on `security/worker-security-packages`;
validation source is `a99aee434daae69da284f5546b1d9a2a748f8899`.
Genuine native jq 1.8.2 and util-linux 2.42.4 packages are installed in the final
native amd64/arm64 workers. Eight tool groups and all 54 worker boundary checks
passed on each architecture. All eight native reviewed-image gates, all six Go
jobs and documentation passed at that source. Tool/isolation reports match the
same-job scanned image IDs and rootfs exactly.
No production tag, release or publication was enabled.

Fresh native Ubuntu Trivy inventory per worker still reports **85 Medium/9 Low**, comprising
**94 package/advisory records and 49 CVEs**. Source fixes for eight CVEs map to
34 Medium records; the remaining 60 records keep individual reviewed dispositions.
Raw matching is unchanged because Ubuntu's advisory database does not publish
these ARTEX fixed versions. This does not establish that every remaining record
is an exploitable vulnerability. The 188 exact policy reviews now bind the real
new package versions (72 updated entries; 68 source-remediation annotations),
with unchanged expiry **2026-12-19T12:00:00Z**. No suppression, new VEX exception,
false source identity or High/Critical waiver was added.

Earlier `375b2c3…` image identities remain historical. [Validation](VALIDATION.md)
records current versus earlier proof; [image policy](IMAGE-SECURITY.md) defines
the unchanged gate behavior.

The sanitized [package validation evidence](../security/worker-package-validation.json)
binds both native reports and exact local image, package, source, ABI and test
identities; the separately recorded local worker ID
is `sha256:3bbf4fe6c9ead8360332893a72770cebdcf59c1f9e979ddcd844a329338ebb40`.

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

The worker pins official upstream **1.8.2** and installs actual native
`jq` and `libjq1` packages at **`1.8.2-0artex1`**. See the
[source manifest](../tools/jq/source.json) for the exact archive URL/SHA256 and the
[package builder](../tools/jq/build-debs.sh). These are ARTEX package revisions,
not official Ubuntu packages. Final native amd64/arm64 installation and runtime validation passed.

Both the executable and its loaded shared library must be replaced together;
putting a new binary in `/usr/local/bin` while an old library/package record
remains is incomplete. Dependencies, dynamic linkage, package ownership,
architecture, licenses and installed file hashes must agree with the actual
runtime. The jq builder uses the inventoried system regex library rather
than hiding another bundled copy. Source/manifests and package provenance belong
in source control; downloaded archives, installed dependencies and built `.deb`
files do not belong in the publication copy.

The util-linux build uses upstream **2.42.4** with the existing
Ubuntu **2.41.3-3ubuntu2.2 packaging/configuration** as its integration basis.
Its actual native source-package version is **`2.42.4-0artex1`**, preserving
required binary-package epochs and login variants. This is a real upstream code
upgrade in ARTEX-maintained Ubuntu-native packages, not an official Ubuntu
security release and not relabeled 2.41.3 bytes.

The original 2.41 backport plan would require a substantial set of active legacy
mount-path fixes and prerequisites beyond the released stable branch. Rather than
publish a partial cherry-pick, the implementation uses the complete upstream source,
retains applicable reviewed Ubuntu-specific patches/configuration, and requires
strict `dh_makeshlibs -c4` symbol/SONAME checks against the existing ABI contract.
Those checks are necessary build guards, not proof of identical runtime semantics.
The review still targets CVE-2026-76642, CVE-2026-78408 and CVE-2026-78410, including
descriptor-lifetime and legacy/new mount paths. Native amd64/arm64 builds,
strict ABI checks, final image gates and live tool/isolation checks passed. The ABI review permits 16 additive functions in three new symbol nodes
and no removed existing symbols; strict `dh_makeshlibs -c4` remains enabled.

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
only from actual new reports, preserving their approved expiry. The measured raw counts and source-fix
classification above remain distinct even after both native gates passed.

## Completed source and native runtime checks

- jq upstream tests: **9 passed**. The SONAME and checksum-pinned **181 exported
  symbol** contract is enforced in each native build; it checks names rather
  than all possible ABI semantics.
- util-linux selected upstream log: **54 test groups passed**, with **93 OK and
  46 SKIPPED result lines** from nested cases. These are different counters.
  Separately compiled tests passed nine mount cases and one nsenter case with
  kernel calls simulated; that is not proof of real privileged mount operations.
- Native amd64/arm64 tool fixtures each: **8/8 groups**, including jq's 17 checks, util-linux's
  17 checks and actual Chromium interaction/rendering.
- Native amd64/arm64 worker fixtures each: **54/54 checks**, including real unprivileged
  `nmap -sT -Pn -n` against an approved synthetic port, root-to-task drop with
  zero capabilities, denied root regain, egress, replay and watchdog/cancel paths.

The util-linux release signature was actually verified over the uncompressed
archive with the full independently pinned maintainer fingerprint recorded in
[source.json](../tools/util-linux/source.json). Ubuntu's packaging archive SHA256
was authenticated through signed APT Sources metadata. These are separate trust
checks; build-time hash pinning does not independently repeat OpenPGP verification.
See the [jq build guide](../tools/jq/README.md) and
[util-linux build guide](../tools/util-linux/README.md) for package and patch details.

## Completion criteria

- Pin original source and every patch to immutable identities/checksums, retain
  attribution/licenses, and verify the patch applies only to expected source.
- Build and inspect architecture-correct packages on native amd64 and arm64;
  verify loaded libraries and package inventory match actual installed files.
- Exercise jq JSON/filter/module behavior and the relevant adversarial fixes;
  exercise util-linux tools used by worker startup and assessment workflows.
- Repeat the expanded **54-check worker boundary** suite, including an actual
  unprivileged `nmap -sT -Pn -n` connection scan of the authorized synthetic port.
  The **eight tool groups** include jq 17, util-linux 17 and real Chromium behavior;
  these are required new integration results, not passes inferred from earlier
  53-check or six/seven-group checkpoints. Repeat privilege, egress and
  cancellation/watchdog checks. Do not relax capabilities, AppArmor, no-new-privileges or target rules
  to compensate for an incompatible package.
- Scan all exact final component images on both architectures with fresh DB
  identity. Record raw findings, proven fixes and residual reviewed risks without
  predicting how many alerts a custom package will remove.
- Refresh stale exact risk reviews from actual results, repeat source/credential
  and archive-manifest checks, and preserve the reviewed release identity policy.

Native results are recorded in [image evidence](../security/image-followup-evidence.json).
Later documentation/evidence descendants require their own CI and final source/ZIP
receipt; these implementation passes do not establish final-main publication.
A successful build or a changed package version alone does not fulfill these
criteria or authorize a formal release.

# Runtime image vulnerability gate

The image gate checks the application, worker, broker and customized PostgreSQL
images that are actually built. An image digest pin makes an input immutable;
it does not establish that its packages are free of vulnerabilities. This gate
adds OS/language package inventory, current advisory matching and an explicit
review process for temporary exceptions. Standard CI also blocks every fixable
Medium/Low issue. Signed publication additionally requires explicit, exact,
expiring review for any active vendor-unfixed Medium/Low worker Ubuntu OS finding.
The operator authorized this narrow residual-risk allowance; it does not authorize
turning on publication, creating a version tag or publishing a release.

## November 19 follow-up status

The worker retains Ubuntu 26.04 and reviewed Node 24.22.3, uses official Python
3.14.10, and installs explicit honest-version
[npm `12.2.3+artex.1`](../tools/npm/README.md) and
[pip `26.3+artex.1`](../tools/python/README.md) forks. npm replaces vulnerable
actual nested dependencies; pip removes its complete unreachable legacy
`pkg_resources` subset and then corrects the exact corresponding SBOM entry.
The remaining inventory and licenses are retained. Unused Pebble and Xvfb are
removed. API/worker/broker binaries are built with Go 1.27.4. See the fork guides
for official source checksums, compatibility scope and actual CLI/security tests.
No version-only fix or finding suppression is claimed.

The **26 image-gate regressions**, 15 signing tests, 15 publication tests and both
package-manager source-runtime suites passed. The release driver requires
`reviewed-findings` on the top-level summary and every image; standard and
strict-diagnostic summaries cannot authorize publication.

The actual final four-image **ARM64 reviewed-findings scan passed**, with zero
blocking and 94 exact risk reviews applied. Worker raw inventory remains
**85 Medium + 9 Low active records**, all without `FixedVersion`; application,
broker and PostgreSQL have zero reported findings at every severity. The operator
explicitly accepted the contained, expiring worker residual risk; it is not fixed
or a zero-active result. No withdrawal classification was used.
[Sanitized evidence](../security/image-followup-evidence.json) retains all 94
records (49 unique IDs), applied reviews and authenticated image/DB/report/SBOM
identity. The DB updated at `2026-11-19T07:06:49.809946527Z`, SHA256
`ebba0a2f709d971b1b49f06569bb7cded629f9f87a5cf71bb5cb8dec015a07c0`.

The final rebuilt implementation passed **53/53 live worker checks**, **6/6 actual
worker/browser checks**, the **22-check backup integration** and a fresh isolation
race run in **8.176 seconds**, including the concurrent-removal/cleanup-failure
fix. The six tools/browser checks used the exact scanned worker
`sha256:859d81ed57517566bfde55f03f02137c4c50b7c29a855c1c131d416922b3f9b2`.
The earlier strict diagnostic refusal on the older `8e4ca64…` worker remains
separate historical proof; it is not the current-image reviewed-mode result.

The initial native amd64/arm64 [image run](https://github.com/jaysonguglietta/artexV2/actions/runs/44531004586)
also passed standard-mode inventory: the same 94 worker OS records/versions on
each platform, other components zero, all 16 artifact hashes verified. Its newer
DB identity and exact images are retained separately in the evidence JSON. This
is not a hosted reviewed-mode pass. The [Go run](https://github.com/jaysonguglietta/artexV2/actions/runs/44531004483)
failed PostgreSQL startup in integration/Compose despite passing worker/tool and
frontend/unit/security/recon checks. Diagnosis, reviewed-mode native rerun, OIDC
and a fresh source credential snapshot remain pending in [validation](VALIDATION.md).
These results do not authorize enabling or publishing a release. No enabling
variable, version tag or production release was created. Local scanning remains
available under the documented supported boundary.

## Local use

Build all four components from the reviewed source. The `postgres` target keeps
the digest-pinned PostgreSQL 16.16 engine and replaces its obsolete Go-built
startup helper with Alpine's native `su-exec`. The fixed PostgreSQL mount bootstrap
verifies its preallocated ext4 image and drops all capabilities before its nonroot
supervisor/server run. [PostgreSQL setup](POSTGRES-BACKUP.md) requires building
this image and running `sh scripts/setup-postgres.sh` before Compose startup.
The helper primes the normal confinement profile without touching database volumes.
See [worker setup](WORKER-ISOLATION.md) for the other execution boundary.
Scan locally available images; the scanner does not resolve
a mutable registry tag behind the operator's back:

```sh
docker build -f Dockerfile.source --target application -t artex-local:hardened .
docker build -f Dockerfile.source --target worker -t artex-local:worker .
docker build -f Dockerfile.source --target broker -t artex-local:broker .
docker build -f Dockerfile.source --target postgres -t artex-local:postgres .
python3 scripts/scan-images.py --output /tmp/artex-image-review \
  --image application=artex-local:hardened \
  --image worker=artex-local:worker \
  --image broker=artex-local:broker \
  --image postgres=artex-local:postgres \
  --platform linux/arm64
```

The default Compose API tag is `artex-local:development`; use that reference if
scanning a Compose-built application instead of the explicit test build above.
Use `linux/amd64` for an amd64 image set. The platform option is optional, but when
provided every inspected image must match it. Components may be scanned
individually with one or more `--image` arguments. A full release needs all four
components on every published architecture. Use a new or empty output directory.

For production eligibility, use the same images with `--require-reviewed-findings`.
The [release image driver](../scripts/release-images.py) must use this mode before
any image push, and requires that mode in the summary and each component record;
a standard or strict-diagnostic summary cannot authorize publication. It fails
for any active residual finding without an exact valid review. Use the independent
`--require-no-active-findings` diagnostic when a zero-active policy is wanted; it
blocks reviewed active findings too. Inspect the recorded `gate_mode` and review
dispositions, not exit code alone, when comparing different modes.

The default tool location is `~/.cache/artex-security-tools/trivy-0.76.2`; the DB
cache is `~/.cache/artex-trivy`. Override them with `--trivy PATH` and
`--cache-dir DIR`. `scripts/install-trivy.py --output PATH` installs separately
when needed. Scan output is private local review material, not source code to
commit or upload blindly.

## Tool, database and image identity

[Tool pins](../security/tool-pins.json) select official Trivy v0.76.2 archives and
exact SHA256 values for macOS ARM64, Linux amd64 and Linux ARM64. The installer
bounds downloads and archive members, verifies the archive before parsing, and
copies only its regular `trivy` executable. It never extracts an entire tar tree
or executes a remote installer. Existing binaries must match a pinned executable
digest or the executable in a freshly verified release archive; a local receipt
alone is never authoritative. Unsupported hosts fail rather than choosing an
unverified artifact.

The scan resolves each local reference to a Docker image ID and platform, then
exports that exact ID to a private temporary tar. Trivy reads the archive without
using a Docker socket. For a classic Docker image, the config digest directly authenticates the inspected
ID. For Docker's containerd/OCI store, the gate verifies the export index, selected
platform manifest, config and layer descriptor hashes back to the inspected index
ID. Config diffIDs must equal Docker's inspected rootfs, and each exported layer
is hashed/decoded within bounds without extraction. Trivy must report the resulting
authenticated config digest, exact diffIDs, platform and scanner version; an OCI
index ID is never loosely substituted for a different config ID. Both the exported
original ID and selected config digest are recorded in the summary. The tar is removed after use. Tags can change later without
changing which image was checked. Summary and artifact hashes preserve the
relationship between the decision, package report and SBOM.

Every `TRIVY_*` environment variable is removed from the scanner environment.
Explicit empty config, ignore-file and Rego-policy arguments prevent ambient
files from masking findings. The scanner uses an empty Docker credential-config
directory; it does not inherit the Docker client's context/socket variables.
Vulnerability scans use offline dependency analysis after loading the database.
The official public database may be downloaded when missing or stale; this is
not a cloud upload of the image or its contents.

Database metadata must use schema version 2, have valid timezone-bearing dates,
and be no more than 72 hours old by its `UpdatedAt` value. A future timestamp,
missing DB, failed refresh or unreadable metadata fails the gate. The summary
records the repository, metadata and hashes of the actual DB/metadata bytes; the
DB must remain unchanged throughout the run. A recently downloaded stale DB does
not pass merely because its `DownloadedAt` value is new.

## Gate decisions and coverage

The full JSON report retains all severities, unfixed vulnerabilities, packages
and scanner-suppressed records. CycloneDX SBOMs are converted from that same
inventory. The policy blocks every active or unverified **High**, **Critical** or **Unknown**
severity finding, including issues without a published fix. With the required
`block_fixable_lower_severity: true` policy, it also blocks every active Medium/Low
record with a fixed version. Vendor-unfixed Medium/Low remain explicitly counted
and visible. A standard-mode pass alone is not risk acceptance or a clean-scan
claim. Exact temporary residual-risk reviews and independently verified withdrawn
advisories are distinct dispositions;
raw severity counts always remain visible. Unknown or malformed
vulnerability records are errors rather than a clean result. Trivy ignore/status
filters cannot act as independent approvals.

**Reviewed-publication mode** (`--require-reviewed-findings`) also blocks every
**unreviewed** active Medium/Low record. Its only allowance is an exact policy
review of a worker `os:ubuntu:26.04` finding with matching CVE, package, installed
version, severity and platform, no listed fixed version, a primary Ubuntu CVE
reference, concrete containment and a valid review/expiry window of at most
30 days. It cannot waive High/Critical/Unknown, fixable findings, another
component/OS target, changed severity/version or an unreviewed architecture.
Expired/malformed/stale reviews and incomplete scans fail closed.

**Independent strict diagnostic** (`--require-no-active-findings`) blocks all
active records, including reviewed vendor-unfixed Medium/Low. It deliberately
ignores reviews, including expired/stale entries, so they cannot mask inventory
behind an exception-validation error. Its earlier actual
94-record refusal is retained as historical negative-test evidence, not a claim
that reviewed publication passed. Verified primary-source withdrawals retain
their separate proof/status/raw counts; withdrawal is not acceptance of an active
issue. Missing/ambiguous proof and scanner errors still fail closed in every mode.

EOL operating systems, missing package inventories, unexpected platforms and
scanner/report mismatches fail. The worker requires Ubuntu 26.04; broker and
PostgreSQL require Alpine inventories. The minimal application is an explicit
exception to OS detection: it must inventory the exact `app/artex` Go binary,
including the application module and Go standard library. Its summary states
that an OS package scan is not applicable. Missing OS metadata on another image,
or a stripped/unrecognized application binary, fails. Worker inventory must also
include `usr/local/bin/tool-worker` with the application module and versioned Go
standard library, plus versioned npm, playwright and playwright-core packages.
Broker inventory must include its worker-broker application module/stdlib and
`usr/local/bin/docker` with versioned stdlib. A clean OS-only base image cannot
stand in for these complete components. The application image keeps
only the static Go service and required certificate/time-zone/skill data; Node,
Python, browsers and assessment tools belong in the worker image. This does not claim that
an OS package manager exists inside a scratch image.

For each component the output contains:

- `COMPONENT.trivy.json`: complete package and vulnerability report.
- `COMPONENT.cdx.json`: CycloneDX package SBOM from the report.
- `summary.json`: exact image IDs/platforms, tool and DB identity, full severity
  counts, blocking findings, applied exceptions, coverage and final decision.

Exit code zero means the selected images passed this policy. Nonzero includes
blocked findings, scanner failures, stale DBs and invalid/stale exceptions. Inspect
`status` and `errors`; an empty or partial report is never proof of a clean scan.
Vendor-unfixed Low/Medium remain visible and require exact valid reviews in
reviewed-publication mode. The strict diagnostic blocks them even when reviewed;
fixable Low/Medium block every mode. This gate
is an advisory/inventory check, not exhaustive source analysis, exploitability
proof, kernel escape testing, malware detection or a secret scan. The official
Docker CLI report currently exposes Go stdlib inventory without its full vendored
module graph; this limitation is not represented as complete Docker-source review.
PostgreSQL and the copied Node engine also need separate supported-version checks;
OS/npm package inventories do not fully catalog those native engine binaries.

## Exact temporary residual-risk reviews

[Image policy](../security/image-policy.json) is reviewed source, not a scanner
ignore file. Fix packages or remove unused software first. The operator has
authorized narrowly reviewed vendor-unfixed Medium/Low **worker Ubuntu 26.04 OS**
records; this accepts residual risk within documented containment rather than
claiming those vulnerabilities are repaired. No other class is eligible.

The current JSON contains **188 exact reviews: 94 per native platform**, each
with 85 Medium/9 Low. ARM64 was reviewed `2026-11-19T13:04:35.549172Z`; amd64 was
added only after actual hosted inventory verification, reviewed
`2026-11-19T13:19:24.496638+00:00`. All expire `2026-12-19T12:00:00Z`, within
30 days, and explicitly match severity and `fixed_version: null`. The initial
hosted standard reports and local reviewed report keep their earlier policy
hash; fresh reviewed-mode hosted CI is pending after this configuration change.
Publication has not been enabled and no release/tag was created.

Each review must include `fixed_version: null` and name one exact CVE, package,
installed version, severity, worker
component, `os:ubuntu:26.04` target and `linux/amd64` or `linux/arm64` platform.
It must match a fresh report without a fixed version. Require a primary Ubuntu
CVE reference, a concrete exposure/containment reason, review timestamp and expiry
no later than 30 days after review. A valid ARM64 entry cannot approve amd64.
Changes in package version, severity or advisory inventory require a new review;
new findings fail reviewed publication until explicitly reviewed. No wildcard,
package-wide ignore, indefinite renewal, fixable-risk waiver or `ignore-unfixed`
is permitted.

Example schema only; never insert this synthetic review into the active policy:

```json
{
  "vulnerability_id": "CVE-2026-12345",
  "package": "exact-package-name",
  "installed_version": "exact-installed-version",
  "severity": "MEDIUM",
  "fixed_version": null,
  "component": "worker",
  "target": "os:ubuntu:26.04",
  "platform": "linux/arm64",
  "reviewed_at": "2026-11-19T13:04:35.549172Z",
  "expires": "2026-12-19T12:00:00Z",
  "reason": "The untrusted per-run worker has target-only egress, no control-plane credentials or host mounts, finite resources, a read-only root and bounded cancellation/watchdog cleanup. This is temporary containment, not a package fix; reassess the vendor patch and exact image before expiry.",
  "reference": "https://ubuntu.com/security/CVE-2026-12345"
}
```

For OS packages the stable target is `os:FAMILY:VERSION`, using the exact detected
version; temporary tar paths are never exception keys. This policy allows reviews
only for `worker` and `os:ubuntu:26.04`; logical language targets such as
`app/artex` or `usr/local/bin/docker` are inventoried but ineligible for this
allowance. Use the actual target rather than guessing. The reference is reviewed by
the approver; a syntactically valid URL alone does not establish advisory status.
A static risk exception is not used merely because an advisory was withdrawn.
For blocking `node-pkg` findings only, the gate can check an exact CVE/GHSA against
the fixed public `api.github.com` advisory endpoint. Every returned record must
identify the requested CVE/GHSA and exact npm package and have a nonfuture
`withdrawn_at`. Active, missing, ambiguous, wrong-package, mismatched or unavailable
evidence leaves the finding blocking. Redirects are refused, no credentials are
sent, and report-provided URLs are never fetched. One bounded primary response is
cached per identifier within the run; no persisted approval cache is trusted.
The summary records `verified_withdrawn`, the request/time/response hash and official
records alongside the unchanged raw finding. This is a point-in-time advisory
status classification, not a package upgrade or acceptance of an active risk.

In standard and reviewed-publication modes, expired, malformed, duplicate and
unknown-component exceptions fail. The independent strict diagnostic ignores
review entries and evaluates every active finding without applying them. An exception
that no longer matches a finding on a selected component/platform is stale and
fails too. Exceptions for an unselected component or another platform are not
required to match that partial scan. Renewals require a fresh review; do not
silently extend expiry in automation.

## CI and maintenance

[Runtime image security](../.github/workflows/image-security.yml) builds and scans
all four components on native amd64 and ARM64 runners for pushes, pull requests
and a weekly schedule. It needs only `contents: read`, publishes no images and
uploads report/SBOM artifacts for 30 days. It has no signing token, OIDC permission
or untrusted SARIF upload. Image/release publication must separately require the
reviewed-findings gate for the exact architecture-specific image IDs it will
publish. Both native image jobs also run actual browser/Python/npm tool behavior
checks; configured workflow steps are not proof that a new hosted run passed.
Current source proof and pending runs are separated in [validation](VALIDATION.md).

Run the offline gate regressions before changing policy or tooling:

```sh
python3 -I scripts/test-image-security.py
```

Tests cover missing/malformed inventory, DB freshness, EOL OS, exact image/platform
identity, fixable/unfixed findings in standard versus strict mode, unknown
vulnerabilities, suppressed records, exception expiry
and scope, ambient filtering tampered/nonregular tool artifacts, and strict official-withdrawal identity/failure
cases. Real image
scans remain necessary after Dockerfile, dependency, base-image or DB changes.
A prior clean report does not substitute for a fresh release gate.

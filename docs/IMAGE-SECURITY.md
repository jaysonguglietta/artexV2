# Runtime image vulnerability gate

The image gate checks the application, worker, broker and customized PostgreSQL
images that are actually built. An image digest pin makes an input immutable;
it does not establish that its packages are free of vulnerabilities. This gate
adds OS/language package inventory, current advisory matching and an explicit
review process for temporary exceptions.

## Local use

Build all four components from the reviewed source. The `postgres` target keeps
the digest-pinned PostgreSQL 16.16 engine and replaces its obsolete Go-built
startup helper with Alpine's native `su-exec`. The entrypoint still drops root
before starting PostgreSQL. See [worker setup](WORKER-ISOLATION.md).
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
severity finding, including issues without a published fix. Exact temporary risk
exceptions and independently verified withdrawn advisories are distinct dispositions;
raw severity counts always remain visible. Unknown or malformed
vulnerability records are errors rather than a clean result. Trivy ignore/status
filters cannot act as independent approvals.

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
Low/Medium findings remain visible and still need engineering review. This gate
is an advisory/inventory check, not exhaustive source analysis, exploitability
proof, kernel escape testing, malware detection or a secret scan. The official
Docker CLI report currently exposes Go stdlib inventory without its full vendored
module graph; this limitation is not represented as complete Docker-source review.
PostgreSQL and the copied Node engine also need separate supported-version checks;
OS/npm package inventories do not fully catalog those native engine binaries.

## Temporary reviewed exceptions

[Image policy](../security/image-policy.json) starts with no vulnerability
exceptions. Fix packages or remove unused software first. An exception must name
one exact CVE, package, installed version, component, stable scan target and
platform. It needs a concrete justification, a reviewed primary HTTPS reference,
a review timestamp and an expiry no later than 30 days after review. Do not use a
wildcard, package-wide ignore, indefinite suppression or `ignore-unfixed`.

Example schema only; do not add this synthetic entry to the active policy:

```json
{
  "vulnerability_id": "CVE-2026-12345",
  "package": "exact-package-name",
  "installed_version": "exact-installed-version",
  "component": "worker",
  "target": "os:ubuntu:26.04",
  "platform": "linux/arm64",
  "reviewed_at": "2026-11-18T20:00:00Z",
  "expires": "2026-11-25T20:00:00Z",
  "reason": "State the precise exposure analysis, compensating control and upgrade plan.",
  "reference": "https://github.com/advisories/GHSA-aaaa-bbbb-cccc"
}
```

For OS packages the stable target is `os:FAMILY:VERSION`, using the exact detected
version; temporary tar paths are never exception keys. Language package targets
are the exact logical Trivy targets such as `app/artex` or `usr/local/bin/docker`.
Use the target in the summary rather than guessing. The reference is reviewed by
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

Expired, malformed, duplicate and unknown-component exceptions fail. An exception
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
same gate for the exact architecture-specific image IDs it will publish.

Run the offline gate regressions before changing policy or tooling:

```sh
python3 -I scripts/test-image-security.py
```

Tests cover missing/malformed inventory, DB freshness, EOL OS, exact image/platform
identity, unfixed/unknown vulnerabilities, suppressed records, exception expiry
and scope, ambient filtering tampered/nonregular tool artifacts, and strict official-withdrawal identity/failure
cases. Real image
scans remain necessary after Dockerfile, dependency, base-image or DB changes.
A prior clean report does not substitute for a fresh release gate.

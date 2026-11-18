# Validation record

Date: 18 November 2026. Results below are point-in-time observations with explicit
source scope. Fresh local integration, runtime image and targeted follow-up
checks have passed, including final Compose acceptance and a fresh prepared-source
credential scan. Hosted OIDC/amd64 remain pending as listed below. Earlier
integration counts and image IDs must not be represented as results for later edits. No local
pass implies that GitHub Actions, real OIDC signing or release publication passed.

## Current targeted follow-up checks

| Check | Observed result | Scope and limits |
|---|---|---|
| Frontend tests | 26 passed in 8 files | Includes false/missing update capability and clipboard-failure regressions; no in-process apply/rollback call occurs. |
| Clean-source TypeScript | Passed | Temporary source tree without generated Next files; verified dependencies matched the lockfile. No dependency symlink was left in publication source. |
| English catalog | Passed, 3,086 keys | English is the only application locale. |
| Biome | 0 errors, 124 warnings, 659 informational diagnostics | Existing warnings and class-format recommendations remain. |
| Image-gate regressions | 19 passed | Tool/archive tampering, missing inventories, DB freshness, EOL OS, exact platforms, OCI/config/layer identity, active/unknown/unfixed findings, scoped expiry and live withdrawal identity/failure cases. |
| Local signing/publication regressions | 14 signing + 15 publication tests passed | Signature/metadata/path/publication negatives use local cryptographic fixtures; they do not reproduce the hosted Fulcio trust chain. |
| English source gate | 480 exact reviewed technical-data lines; zero unreviewed/stale matches | Preserves evidence, provider signatures and compatible wire/migration inputs; no whole-file exemptions. |

The minimal API contains the static Go service and certificate/time-zone/skill
data, without the worker's assessment toolchain. The worker base is Ubuntu 26.04
with reviewed Node/npm/browser tooling. Production in-process apply/rollback is
permanently refused, including with the legacy enabling flag. Read-only version
checks remain available; installation is an external signed-verification workflow.

## Final local follow-up verification

These results cover the storage/image/release changes and current release UI.
They are separate from the older checkpoint below. Image identities and database
provenance are recorded in the next section; hosted run evidence is still pending.

| Final-source check | Observed result / remaining scope |
|---|---|
| Disposable PostgreSQL integration | **869 passed, 0 failed, 1 optional live-provider skipped**, including subtests; **13 additional tested Go suites passed**. The skip is not counted as a pass. |
| Full focused races | Passed for `storage`, `traffic`, `isolation`, `evidence`, `agent` and `server`. |
| Go vet and Windows cross-build | `go vet ./...` and final windows/amd64 API cross-build passed. |
| Current source-image builds | All four passed: application, worker, broker and customized PostgreSQL; includes the current embedded static frontend. Fresh export inspection checked 30 HTML pages with no unintended CJK text. |
| Worker boundary | 52/52 live checks passed against disposable synthetic targets. |
| Detection regressions | Eight suites and three supporting gates freshly passed against the updated source and synthetic fixtures. |
| API security smoke | 12/12 passed against the rebuilt application. |
| Four runtime image gates | All linux/arm64 components passed with **0 blocking findings**; complete raw counts, required inventories, config/layer bindings and live withdrawal proof are retained below. |
| Source dependency checks | Fresh govulncheck reported 0 reachable findings; npm reported 0 findings. Neither result implies complete OS/native-engine or all-build-tag coverage. |
| Storage/context behavior | Covered by the fresh PostgreSQL suites and race runs, including new quota/retention/hold and context/transcript paths; this is not an independent forensic or deletion certification. |
| Fresh Compose startup/persistence | **11/11 passed**, including native PostgreSQL UID drop, restart persistence, absent API interpreters, API-to-broker tool execution and fixture cleanup. |
| Final prepared-source credential scan | Pinned Gitleaks v8.30.1 scanned the fresh prepared source with no network: six individually reviewed synthetic fixture matches, zero unresolved matches and zero confirmed credential leaks. Publication snapshot changes require another scan. |
| Remote amd64 build/image gates | **Pending** hosted CI after the source push. Local arm64 results are not cross-architecture results. |
| Real main-only keyless rehearsal | **Pending** hosted OIDC/Fulcio/Rekor run and strict positive/negative verification. Local signing tests do not substitute for it. |
| Protected-tag production release | Not claimed. Requires exact tag/commit/run evidence, hosted protections, complete gates and verified publication. |

## Runtime image review status

All four final **linux/arm64** gates passed. These are raw database severity
counts, not counts after hiding a withdrawn record:

| Component | Critical | High | Medium | Low | Unknown | Verified withdrawn | Blocking |
|---|---:|---:|---:|---:|---:|---:|---:|
| application | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| worker | 0 | 1 | 132 | 14 | 0 | 1 | 0 |
| broker | 0 | 0 | 1 | 0 | 0 | 0 | 0 |
| postgres | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

The worker's High is Picomatch `CVE-2026-112366`, installed version `4.0.5`.
The live official API proof at `2026-11-18T21:10:47.535590Z` matched that exact
npm/CVE identity to
[GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf), withdrawn at
`2026-11-18T15:38:56Z`. The report retains raw High 1 and the full finding/proof
as `verified_withdrawn`, distinct from a risk exception. No static exception,
`ignore-unfixed` or package-wide suppression was used. Active, mismatched,
ambiguous or unavailable evidence would still block. This is a point-in-time
advisory-status classification, not a claimed package fix. Medium/Low findings
remain visible and need continuing review.

Trivy v0.76.2 used DB schema 2 from `ghcr.io/aquasecurity/trivy-db:2`, with
`UpdatedAt=2026-11-18T13:03:23.615586156Z`. The exact DB SHA256 was
`7244f40ff53c4ac2909d8fba04aa4937354a620c380dc83bbc51df715f6cd68d`;
metadata SHA256 was
`5fe01541096db5ac8b1dcd69aebc4b6a78b7274d8b042f9809854fe4bbce4f6c`.
The DB predates the withdrawal, so a fresh primary API proof was required. Future
runs must recheck DB freshness and advisory status rather than reuse this result.

### Exact final local ARM64 identities

These local export IDs can be OCI index IDs. Each is authenticated through its
selected manifest/config/layers; Trivy must match the config digest and inspected
rootfs diffIDs. They are not claims of published registry digests.

| Component | Original Docker image ID | Authenticated config digest |
|---|---|---|
| application | `sha256:e9cbc5cd52d1255a88ef41762ae2b375311c52cd95544eaf07dd7f1853558972` | `sha256:d6c37bb971d731b55fb558b94f3183a7ca8eee1e9a12b8c3e778efbc7f86d707` |
| worker | `sha256:177ffe1cb988abb7173d4f986d8b02b745edc1707f4c25e9fef38a0f611350f7` | `sha256:b6ceb4b7690ddc9e81ed811002cd750f44631ee01739b573bf8d0fdb1d58fa43` |
| broker | `sha256:e2046ed7f14b4828464c64990c956d815e7252db4e16b8e291065cc2556adfb2` | `sha256:fec563a27aeef8ac25ab19c53b79ff304bc47e25c31694f11c5299e62072a4a0` |
| postgres | `sha256:d24e3cf8e8836fca127886e9dff241283dd62aff9790d86daf4fc3f002b6601e` | `sha256:fa4630afb3a9b0df186a902f405451c4a9c9d054f6a1c67749c6705d85ccdb3a` |

The minimal application has no detected OS package inventory; its application Go
binary/module/stdlib are inventoried. Worker is Ubuntu 26.04; broker and customized
PostgreSQL use Alpine 3.24.4. PostgreSQL keeps the pinned 16.16 engine and replaces
the old Go-built privilege helper with native `su-exec`. The final Compose fixture
verified initialization, native privilege drop and persistence through a restart.

The gate was also exercised against known-vulnerable earlier broker/PostgreSQL
images and correctly blocked 33 findings in total. A passing OS-only tools base
cannot substitute for a complete component: required worker/broker Go and npm/browser
inventories are checked. The official Docker CLI exposes stdlib rather than its
full vendored Go graph, and native engine versions need separate checks; no
all-CVE or all-runtime-code guarantee is inferred. See
[image security](IMAGE-SECURITY.md) for these limits and complete gate semantics.

## Earlier completed integration checkpoint

These observations predate the latest storage/image/release changes. They remain
useful earlier regression evidence; the fresh local results above supersede their
counts for the current source. Still-pending hosted/snapshot checks remain explicit.

| Check | Earlier observed result | Scope and limits |
|---|---|---|
| Disposable PostgreSQL integration | 817 passed, 0 failed, 1 skipped | Six database-backed packages including subtests; ten additional package suites passed. The optional live-model test needs credentials and is not counted as passed. |
| Worker Docker boundary | 52/52 passed | Disposable worker/broker containers and synthetic local targets, with cleanup. |
| Fresh Compose startup | 8/8 passed | API/PostgreSQL/broker setup, bridge creation/reuse, authentication, tool call and cleanup. |
| API container security smoke | 12/12 passed | Sessions/cookies, same-origin requests, body bounds, host execution/update gates, query-token refusal and logout/bearer revocation. |
| Focused races, Go vet and Windows build | Passed | Default host configuration plus windows/amd64 API compilation; Linux/Docker broker is separate. |
| Application/worker/broker builds | Passed | Then-current source, including embedded static frontend; earlier IDs below. |
| Earlier frontend tests/catalog | 23 tests in 7 files; 3,080 keys | Superseded by current 26-test/3,086-key targeted checks above. |
| Earlier static frontend export | 30 HTML pages checked; no unintended CJK | Predates the new read-only release UI. |
| Upload preparation | 6 regressions passed | Exclusions, no-follow access, identity race, modes, deterministic archive and overwrite protection. |
| Worker setup | 5 regressions passed | Bridge setup/reuse, unsafe/conflicting network refusal and secret-only mode. |
| npm advisory classification | 4 regressions passed | Active/unknown/unverifiable leaves fail; verified withdrawal remains distinct from a package fix. |
| Recon helper | 7 policy and 3 Chromium tests passed | Synthetic loopback servers; networkless read-only browser fixture. |
| Detection checks | 8 suites and 3 gates passed | Synthetic Sigma/Suricata, matching/lint, ATT&CK, indicators, MISP and harness/triage/template coverage. |

## Worker adversarial coverage

The live fixture checks allowed target access while denying neighboring targets,
gateways and management networks. It verifies the tool process UID, capability
sets, no-new-privileges, read-only image and absence of API secrets or a Docker
socket. Root bootstrap authority is dropped before tool input is accepted.

Lifecycle tests cover urgent cancellation during active foreground work, refusal
to reuse closed IDs, a surviving sibling run, and detached setsid/double-fork
descendants. A separate root-owned PID1 watchdog exits after lost renewals; the
worker user cannot signal or renew it. The tests observe network activity stop
after cancellation and lease expiry.

Workspace tests exercise real tmpfs artifacts, all-sibling snapshots, conflicts,
oversized preservation failures, resumption after failure and task retirement.
Additional Go regressions reject path/inode amplification before any filesystem
mutation: at most 1,024 path bytes, 32 components and 2,048 parent directories.
Empty-directory walks are bounded and invalid snapshots cannot delete originals.
Crash tests verify owner-labeled orphan reaping and persistent replay protection
after a broker restart. These tests do not establish forensic atomicity of
snapshots taken from an actively malicious writer.

The disposable fixture uses a host-network helper to install synthetic target
addresses inside its Docker host. Run it only against a dedicated assessment/test
daemon. It never needs production target addresses or credentials.

## Earlier dependency and credential checks

Official govulncheck v1.10.0 reported no reachable findings for the then-checked Go
source graph: 52 modules and 20 root packages, Go 1.27.4, DB last modified
11 November 2026. Test-only code, alternate tags/platforms and the new final source
are not automatically covered by that earlier result.

The earlier full frontend npm audit, production frontend audit and separately
locked recon/browser helper audits reported zero. An earlier still-older raw full
frontend report had six High dependency-graph entries derived from the same
Picomatch advisory withdrawn above. The npm registry later stopped returning them.
Those six graph entries were not six independent vulnerabilities. Re-run the
recorded dependency checks against the final source; do not generalize an npm
result into OS/native-engine coverage.

Gitleaks v8.30.1 found six generic-key matches, each verified as a synthetic
masking/invalid-authentication fixture. No confirmed credential leak or unresolved
match was found in that prepared snapshot. The official pinned scanner ran locally
with no external network, read-only source/root, no Docker socket/baseline and no
whole-test-directory exclusions. Later source/Git history needs a fresh scan.

## Reproduce

Use the reviewed Go toolchain, frontend Node lockfiles, Python 3 and a dedicated
disposable Docker daemon. The PostgreSQL harness creates its own loopback-only
databases; never point fault-injection tests at a real assessment/production DB.

```sh
./scripts/test-isolated.sh
go test -race ./isolation ./agent ./cmd/tool-worker
go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/artex-windows-check.exe ./cmd/artex
python3 scripts/test-prepare-upload.py
python3 scripts/test-setup-workers.py
python3 scripts/test-audit-npm.py
python3 scripts/test-image-security.py
python3 scripts/test-signed-release.py
python3 scripts/test-release-publication.py
python3 scripts/check-english.py
python3 scripts/check-doc-links.py
npm --prefix web ci --ignore-scripts
python3 scripts/audit-npm.py
npm --prefix web run typecheck
npm --prefix web run check
npm --prefix web run check:i18n
npm --prefix web run test -- --run
npm --prefix web run build:static
python3 scripts/check-web-cjk.py
docker build --target worker -f Dockerfile.source -t artex-local:worker .
docker build --target broker -f Dockerfile.source -t artex-local:broker .
python3 scripts/test-workers.py
docker build --target application -f Dockerfile.source -t artex-local:hardened .
docker build --target postgres -f Dockerfile.source -t artex-local:postgres .
python3 scripts/test-compose.py
```

Use [image security](IMAGE-SECURITY.md) for actual exact-image scans,
[signed releases](SIGNED-RELEASES.md) for external verification and the real hosted
OIDC rehearsal, and [evidence storage](EVIDENCE-STORAGE.md) for quota/retention/hold
configuration and recovery checks. Those guides define the scope of their controls;
no all-CVE, forensic-atomic, certified SLSA or multi-tenant guarantee is implied.

Worker and Compose fixture reports default to private temporary directories. Both
require a disposable daemon; their host-network helper only sets synthetic test
addresses. The earlier 12-check API smoke was an additional local harness, not a
checked-in command; do not claim that exact count from the commands above alone.

## Earlier local image identities

These are content IDs from the earlier integration checkpoint, not final follow-up
images or published registry digests. Rebuilds and base/OS package changes invalidate
them as evidence for a later source revision.

| Image | Local image ID |
|---|---|
| artex-local:hardened | sha256:a874c0bd4958bccecb5f0c6229bafa25fcec67073cc588d9af5b603bbb2970fe |
| artex-local:worker | sha256:09516f614ae3443fa15034b3a0fbe41c2f90d2972a3dd4f080a1e56c22fef3e9 |
| artex-local:broker | sha256:c6e66921d70b9b65dd3e1237e221d3b1c413166ef0039ece4c92d4e29687fb8d |

See [security review](SECURITY-REVIEW.md), [the audit](SECURITY-AUDIT.md) and
[worker setup](WORKER-ISOLATION.md) for findings and supported deployment limits.

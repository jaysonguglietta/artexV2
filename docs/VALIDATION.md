# Validation record

Date: 18 November 2026. This records local verification of the English-only,
isolated-worker publication source. Results are point-in-time observations, not a
claim that the application is free of vulnerabilities. Remote GitHub Actions runs
have their own status; a local pass does not imply that a remote run has passed.

## Results

| Check | Observed result | Scope and limits |
|---|---|---|
| Disposable PostgreSQL integration | 817 passed, 0 failed, 1 skipped | Counts include subtests in six database-backed packages; ten additional package suites passed. The optional live-model test needs external credentials. |
| Worker Docker boundary | 52/52 passed | Real worker/broker containers, synthetic local targets, dedicated disposable test networks/state. Includes fixture cleanup. |
| Fresh Compose startup | 8/8 passed | Actual API/PostgreSQL/broker startup, setup-owned bridge creation/reuse, authentication, isolated tool call and fixture cleanup. |
| API container security smoke | 12/12 passed | Authentication, cookies, same-origin enforcement, body bounds, disabled host execution/update, query-token rejection, logout and bearer revocation. |
| Focused Go race tests | Passed | isolation, agent and cmd/tool-worker packages; database integrations are covered separately above. |
| Go vet | Passed | Default host build configuration. |
| Windows cross-build | Passed | API command compiled for windows/amd64; the isolated broker requires Linux/Docker. |
| Container builds | Passed | Source-built application, worker and broker images, including the embedded static frontend. |
| Frontend TypeScript | Passed | Also checked from a source-only temporary tree without generated Next files. |
| Frontend unit tests | 23 passed in 7 files | Automated tests; not exhaustive interactive accessibility or browser coverage. |
| Frontend Biome | 0 errors, 124 warnings, 658 informational diagnostics | Warnings and formatting recommendations remain maintenance work. |
| English catalog | Passed, 3,080 keys | English is the only application locale. |
| Static frontend export | Passed | 30 generated HTML pages; no unintended CJK text. |
| English source gate | Passed | 480 individually hashed technical-data exceptions, zero unreviewed or stale matches. Preserves Unicode evidence and compatibility tokens. |
| Upload preparation regressions | 6 passed | Exclusions, no-follow file access, identity-race refusal, modes, deterministic archive and overwrite protection. |
| Worker setup regressions | 5 passed | Bridge creation/reuse, conflicting network refusal, failed creation and secret-only behavior; multiple unsafe network variants are covered. |
| Advisory classification regressions | 4 passed | Active/unknown dependencies fail; only independently verified withdrawn advisory leaves are accepted. |
| Recon helper | 7 policy and 3 Chromium tests passed | Synthetic loopback servers; browser fixture ran with no external network and a read-only root. |
| Detection checks | 8 suites and 3 gates passed | Sigma, backend conversion/lint/matching, Suricata, ATT&CK, source indicators, MISP, harness/triage/template checks. |
| Documentation links | Passed | Local Markdown links and explicit anchors; external sites are not exhaustively checked. |

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

## Dependency and credential checks

Official govulncheck v1.10.0 found zero reported reachable findings in the default
source graph: 52 modules and 20 root packages, using Go 1.27.4 and the vulnerability
database last modified 11 November 2026. This excludes test-only code and alternate
build tags/platforms. Later lifecycle edits did not add dependencies.

The final full frontend npm audit, production frontend audit and separately locked
recon/browser helper audits reported zero findings. An earlier raw full frontend
report contained six High dependency-graph entries, all derived from one Picomatch
advisory. GitHub withdrew
[GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf) at
2026-11-18T15:38:56Z. The advisory script verifies the official API withdrawal at
run time when an advisory is reported; it fails on active, unknown or unavailable
evidence. The final registry report no longer includes that advisory. No forced
major downgrade or blanket suppression was needed. This does not establish that
every possible glob pattern is safe. Build and test configuration must be trusted.

Gitleaks v8.30.1 found six generic-key matches, each reviewed as a synthetic
masking/authentication fixture. No confirmed credential leak or unresolved match
was found. Scans use the official pinned image, no external network, read-only
source/root, no Docker socket, no baseline and no whole-test-directory exclusions.
The final prepared snapshot is checked separately before publication. Local
configuration, runtime data and private audit artifacts are excluded from it.

OS-package/container CVEs, complete external SDK source review, signed releases,
kernel escape resistance and behavioral equivalence of translated prompts are not
established by these checks.

## Reproduce

Prerequisites: a supported Go toolchain (the checked builds used 1.27.4), Node 22,
npm, Python 3 and a disposable Linux Docker daemon. Install dependencies only from
the checked lockfiles. The PostgreSQL harness creates and removes its own
loopback-only database; do not point it at an existing database.

```sh
./scripts/test-isolated.sh
go test -race ./isolation ./agent ./cmd/tool-worker
go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/artex-windows-check.exe ./cmd/artex
python3 scripts/test-prepare-upload.py
python3 scripts/test-setup-workers.py
python3 scripts/test-audit-npm.py
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
python3 scripts/test-compose.py
```

Worker test reports default to a private temporary directory and can be directed
with ARTEX_WORKER_TEST_RESULTS. Compose reports use ARTEX_COMPOSE_TEST_RESULTS or
the script's --report option and also default to a private temporary directory.
Both fixtures require a dedicated disposable daemon. The API smoke run was an additional local harness,
not a checked-in executable; repeat its behaviors through the server regression
tests and an independently configured disposable Compose deployment. Do not claim
its exact 12-check result from the commands above alone.

The fresh Compose check additionally caught and fixed two deployment issues:
an unreferenced Compose network was not created, and an unquoted inline tmpfs
mount was split at its commas. Setup now creates and verifies its own dedicated
bridge, and the mount is quoted. The bridge remains outside Compose's lifecycle;
never remove an in-use or pre-existing network as routine cleanup.

## Exact local image identities

These are local content IDs from the tested build, not published registry digests.
Rebuilding later can change them because operating-system repositories change.

| Image | Local image ID |
|---|---|
| artex-local:hardened | sha256:a874c0bd4958bccecb5f0c6229bafa25fcec67073cc588d9af5b603bbb2970fe |
| artex-local:worker | sha256:09516f614ae3443fa15034b3a0fbe41c2f90d2972a3dd4f080a1e56c22fef3e9 |
| artex-local:broker | sha256:c6e66921d70b9b65dd3e1237e221d3b1c413166ef0039ece4c92d4e29687fb8d |

See [the security review](SECURITY-REVIEW.md),
[the comprehensive audit](SECURITY-AUDIT.md) and
[the worker setup guide](WORKER-ISOLATION.md) for findings, deployment instructions
and supported limits.

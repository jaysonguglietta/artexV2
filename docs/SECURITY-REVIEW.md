# Security review and engineering controls

Reviewed on 18 November 2026. ARTEX V2 supports one operator in a dedicated
assessment VM, testing explicitly authorized targets. This review describes
implemented controls, regression evidence and remaining limits. It does not
certify multi-tenant isolation, safe unattended operation or coverage of every
vulnerability class. The [validation record](VALIDATION.md) records the checked
snapshot and results; the [worker guide](WORKER-ISOLATION.md) defines deployment
requirements.

## Architecture and threat model

The Go API authenticates the operator, calls configured model providers, schedules
agents and owns PostgreSQL records, approval state and signing keys. A separate
trusted broker controls a dedicated Docker daemon. Executable shell, filesystem,
custom command/Python/HTTP, interactive-shell and MCP tools run in separate
containers per agent run. The browser uses the API on the same origin.

Protect these assets: operator sessions, provider and integration credentials,
signing keys, assessment scope, database records, uploaded files, findings and raw
request/response evidence. Model responses, tool output, target pages, imported
records, uploads and worker artifacts are untrusted even when their transport is
authenticated.

The review considered:

- An unauthenticated client attempting first-run takeover, credential guessing,
  request-body exhaustion, token theft or cross-origin requests.
- A compromised model endpoint or hostile target attempting tool injection,
  prompt injection, credential extraction, filesystem escape or network pivoting.
- An untrusted integration or MCP server attempting credential forwarding,
  redirects, malformed streams and resource exhaustion.
- A compromised worker attempting sibling access, management-network access,
  persistence after cancellation or privilege recovery.
- A malicious dependency, build input or update artifact compromising deployment.

The API, broker, Docker daemon, kernel and operator account remain trusted. The
broker's Docker authority makes broker compromise equivalent to compromise of
that daemon. This is a deliberate trust boundary, not a container-escape defense.

## Changes and regression map

The severity column describes the reviewed issue before its fix, with the stated
prerequisites. It is not a list of currently exploitable unauthenticated endpoints.

| Reviewed issue | Severity / confidence | Implemented control and regression evidence |
|---|---|---|
| Model connection probe could restore SDK default executable tools | High / High | Explicit empty tool list and disabled background tasks; [connection regression](../agent/connection_security_test.go) exercises a malicious synthetic provider response and verifies no canary is created. |
| Tool execution shared API filesystem, credentials and network authority | High / High for design gap | Separate broker and per-run containers; [isolation tests](../isolation/security_test.go), [lifecycle tests](../isolation/run_lifecycle_test.go) and [live worker checks](../scripts/test-workers.py). |
| Empty artifact paths could amplify small worker responses into excessive API directory creation | Medium / High | Whole-map path, depth and directory budgets before writes/deletions; [artifact budget tests](../isolation/files_budget_test.go). |
| Legacy MCP SSE handoff could forward credentials to another origin | High / High | Same-origin handoff, rejected redirects and bounded parsing; [MCP regressions](../mcphttp/security_test.go). Operator-configured integrations still grant network capabilities. |
| Custom command-template placeholders crossed a shell boundary | High / High | Fixed executable and literal argument interpolation, restricted legacy templates and bounded output; [custom-tool tests](../server/customtool_test.go). Intentional shell/Python tools remain confined to workers. |
| Workspace and skill APIs could follow symlinks outside their roots | High / High | Root-anchored filesystem operations, regular-file checks and private permissions; [workspace tests](../server/files_security_test.go) and [agent skill tests](../server/platform_skill_security_test.go). |
| Recording proxy could be reachable outside the intended local boundary | High / High for configuration | Loopback-only binding and no Compose publication; [traffic security tests](../traffic/security_test.go). |
| Authentication allowed excessive body/CPU consumption | High / High | Bounded auth bodies and concurrency, throttling and request deadlines; [request security tests](../server/security_test.go). |
| Browser tokens were exposed to JavaScript and request URLs | Medium / High | Strict expiring JWTs, HttpOnly cookies, no query tokens and durable logout revocation; [session tests](../server/session_security_test.go). |
| Review or policy-read failures could permit operations | Medium / High | Default-deny model review and refused tool/asset assembly on database errors; [tool policy tests](../server/tool_policy_security_test.go), [asset policy tests](../agent/asset_policy_security_test.go) and approval-lifecycle tests. Existing explicit allow-on-failure settings require operator review. |
| Recon helper could forward cookies/requests beyond the intended origin | High / High | Mock-by-default behavior, explicit bounded same-origin GET/HEAD forwarding, no redirects, private captures and secure TLS/browser defaults; [policy tests](../skills/api-recon/scripts/runtime_policy.test.js) and [browser tests](../skills/api-recon/scripts/runtime_browser.test.js). Independent worker egress remains necessary. |
| Mutable update/build inputs could compromise a personal fork | Medium / High | Reviewed local-source builds, pinned actions/base images and lockfiles; in-process update/rollback disabled by default. Signed release provenance and OS package review remain open. |

Relevant categories include CWE-78 (command injection), CWE-22/CWE-59
(filesystem traversal/symlinks), CWE-918/CWE-201 (SSRF/credential forwarding),
CWE-400/CWE-770 (resource exhaustion), CWE-636 (failing open), CWE-295 (TLS),
CWE-598/CWE-922 (token exposure), and CWE-250/CWE-269 (excess authority).

### Confirmed connection-test vulnerability

The previous connection test called `agentcore.Run` with a nil `Tools` list and
bypass permissions. Norma v0.4.4 restores local shell/filesystem defaults for nil.
A malicious or compromised model endpoint could return a Bash tool call during
an administrator's connection test and execute it in the API process. This needed
an operator-tested hostile provider; it was not an unauthenticated Internet RCE.
Impact included API-accessible credentials and host files.

A synthetic local endpoint reproduced the old behavior using only an owned
temporary canary. The fixed implementation in [agent/provider.go](../agent/provider.go)
uses these options explicitly:

```go
Tools:                  []actool.CoreTool{},
DisableBackgroundTasks: true,
```

Keep the non-nil empty slice: replacing it with nil changes SDK behavior. The
regression checks both the advertised tool schemas and absence of tool execution.
Production agent assembly separately controls late SDK injection, including
Monitor, WebFetch and background defaults. Executable tool paths must remain
routed through the isolated worker boundary after SDK upgrades.

## Worker and file-handling invariants

The broker enforces operator-owned task CIDRs, management/Docker/metadata denies,
resource limits, authenticated run identities and admission bounds. Task coverage
scope is model-editable planning data; it is not the network authorization list.
Workers receive no API database DSN, JWT key, broker token, model-provider key,
Docker socket or arbitrary host mount. Explicit MCP integration credentials are
separate capabilities granted to that integration.

Normal completion, cancellation, background jobs and MCP clients share a trusted
run lifecycle. Close has separate admission from foreground tool RPCs. Admitted
run IDs cannot reopen after close or broker restart. A separate UID-protected
PID1 watchdog bounds orphan execution after lost renewals, including detached
processes. This is bounded cleanup during a control-plane outage, not an
immediate-stop guarantee. Production host execution has no opt-in or fallback.

Staged uploads must be selected by `draft_id`, confined and bounded, then copied
into the task before launch and mapped to worker paths. Copy failures return the
created task ID and pause execution rather than concealing a partial commit.
Artifact synchronization is limited to regular files: 1,000 files and 4 MiB total
per call. Paths are limited to 1,024 bytes and 32 components, with a 2,048-directory
budget. Complete maps are validated before writes or snapshot deletions so empty
deep paths cannot amplify a small response into millions of API directories.
Never raise an API body limit as a substitute for a designed streamed
transfer mechanism. Snapshots are copies of untrusted, concurrently writable
worker data, not atomic forensic acquisitions.

See [isolated task execution](WORKER-ISOLATION.md) for configuration, DNS behavior,
lease timing, replay-ledger maintenance and unsupported network capabilities.

## Sessions, integrations and sensitive data

Use loopback access or a private authenticated HTTPS tunnel/reverse proxy. Browser
cookies are HttpOnly and SameSite=Strict, with Secure required away from loopback.
Cookies are host-scoped rather than port-scoped; dedicate the hostname/browser
profile. Query bearer tokens are rejected. Password changes invalidate prior
sessions and logout records a durable token digest revocation.

MCP TLS validation and same-origin handoff prevent a remote integration from
silently receiving another origin's credentials. Typed model/search integrations
remain trusted control-plane connections; tool-worker network rules do not make
an arbitrary provider endpoint trustworthy. Target-only TLS exceptions must not
be reused for credential/control-plane traffic.

Findings, model traces, captures and exports can contain personal data, target
credentials or sensitive business content. Keep runtime data and backups private,
limit integration permissions, and define retention/deletion for each assessment.
Repository hygiene does not replace runtime storage quotas or an evidence-retention
policy. Central append-only audit, broader quotas and lifecycle-managed retention
remain hardening work.

## Dependency and secret checks

Earlier npm reports contained **six High graph entries from one Picomatch
advisory**, not six independent vulnerabilities. GitHub officially withdrew
[GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf) on
18 November 2026. That earlier report was explicitly reconciled with the official
withdrawal. The latest final full npm report and production-only report both
contain **zero findings** after the registry reflected the withdrawal. See the
[validation record](VALIDATION.md) for the checked snapshot and recorded results.

[scripts/audit-npm.py](../scripts/audit-npm.py) reports raw npm counts, checks the
official advisory API when needed and fails for active, unknown or unverifiable
leaves. No package-wide ignore, forced major downgrade or version spoofing was
used. Build/test globs remain trusted input even when the advisory count is zero.
The unused multilingual extraction dependency chain was removed; the application
uses the English catalog and direct `use-intl` integration.

Official govulncheck v1.10.0 reported no findings in the checked default Go source
graph (52 modules; 20 root packages). Its recorded database was last modified on
11 November 2026. This does not cover all build tags, test-only code or unknown
vulnerabilities. The embedded-UI build is validated separately by compilation.

Gitleaks v8.30.1 ran locally against the prepared source snapshot in a pinned
container, with no network, read-only source, dropped capabilities and no Docker
socket. Its immutable ARM64 image digest was
`sha256:6b2d52241f9d176fc59e6e3b026ea52c98f390070587d9475d6ee2bc64941815`.
Six generic-key matches were individually verified as synthetic masking or
invalid-authentication test inputs. No confirmed credential leak or unresolved
match remained in that snapshot. Values were redacted; no whole-test-directory
suppression was used. A later source change requires another scan; this is not a
scan of future Git history.

## Completed validation

| Check | Recorded result |
|---|---|
| Disposable PostgreSQL integration suites | 817 test cases passed, including subtests; 1 optional live-provider test skipped without external credentials |
| Live isolated-worker boundary checks | 52 passed against local disposable Docker targets |
| Container API/security smoke checks | 12 passed |
| Frontend tests | 23 passed across 7 files |
| Frontend TypeScript and English catalog | Passed, including a clean source-only TypeScript check |
| Frontend static build and exported English UI | Passed; 30 exported HTML pages checked |
| Biome | 0 errors; 124 warnings and 658 informational diagnostics retained |
| Go static analysis and portability | `go vet ./...` passed; Windows cross-build passed |
| Dependency and source-secret review | Results and limits described above and in the validation record |

These results demonstrate the checked behaviors, not a security certification.
The skipped live-provider test is not counted as a pass. Unit/integration tests
and synthetic adversarial fixtures do not establish real-target assessment
quality or prompt-translation benchmark equivalence.

## Reproduce validation

Use Go 1.27.4, Node 22 and a dedicated disposable Docker daemon. Install the locked
frontend dependencies without lifecycle scripts before frontend checks:

```sh
./scripts/test-isolated.sh
go test -race ./isolation ./agent ./cmd/tool-worker
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...
python3 scripts/test-prepare-upload.py
python3 scripts/test-setup-workers.py
python3 scripts/test-audit-npm.py
python3 scripts/audit-npm.py
python3 scripts/check-english.py
python3 scripts/check-doc-links.py
npm --prefix web ci --ignore-scripts
npm --prefix web run typecheck
npm --prefix web run check
npm --prefix web run check:i18n
npm --prefix web run test -- --run
npm --prefix web run build:static
python3 scripts/check-web-cjk.py
# Build the worker/broker images as described in WORKER-ISOLATION.md, then:
python3 scripts/test-workers.py
# After also building the application image as artex-local:hardened:
python3 scripts/test-compose.py
```

The PostgreSQL harness creates disposable databases and does not use an existing
production DSN. Its host-execution compatibility flag is accepted only in test
binaries; it is not a deployment option. The optional live-provider test requires
external credentials. Worker tests temporarily assign synthetic benchmark addresses
to the dedicated Docker VM and clean them up; never use a shared/production daemon.

For an individual Windows compilation check, send the binary to a private
non-source output directory and keep it out of the upload copy:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/artex-check.exe ./cmd/artex
```

The [validation record](VALIDATION.md) is the result inventory; commands here are
reproduction guidance, not a claim that every optional environment is available.

## Remaining limits and remediation priorities

1. Preserve the dedicated VM/daemon boundary. A compromised API, broker, kernel,
   Docker daemon or operator remains outside the worker isolation guarantee.
2. Add OS-image/package CVE scanning and signed build/release provenance before
   treating generated images as a production release. Digest pins do not review
   every installed OS package or eliminate upstream compromise.
3. Add centralized append-only audit, operational quotas and explicit retention
   for traces, captures, artifacts and backups.
4. Design a bounded distributed traffic-capture/import pipeline if required.
   Workers cannot use the API recording proxy, privileged raw-packet scans are
   unsupported, and oversized unsynchronized files remain ephemeral.
5. Re-run boundary regressions, secret/advisory scans and measured model behavior
   checks after SDK, deployment or prompt changes. Preserve original evidence and
   technical compatibility data rather than rewriting them for a clean text scan.

[Secure deployment](SECURE-DEPLOYMENT.md), [English conversion](ENGLISH-CONVERSION.md)
and [source upload](../UPLOAD.md) describe the corresponding operator workflows.

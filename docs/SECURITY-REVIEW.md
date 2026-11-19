# Security review and engineering controls

Reviewed on 18 November 2026. ARTEX V2 supports one operator in a dedicated
assessment VM, testing explicitly authorized targets. This review describes
implemented controls, regression evidence and remaining limits. It does not
certify multi-tenant isolation, safe unattended operation or coverage of every
vulnerability class. The [validation record](VALIDATION.md) records the checked
snapshot and results; the [worker guide](WORKER-ISOLATION.md) defines deployment
requirements.

## PostgreSQL bootstrap/client boundary — fixed and verified

The current PostgreSQL hardening changes are **not covered** by the earlier
local backup/Compose/image checkpoints below. Native reviewed-image
[run44532828731](https://github.com/jaysonguglietta/artexV2/actions/runs/44532828731)
passed all eight images at `1ea9d66`, before the final entrypoint/client changes.
A later attempt [failed `pg_ctl` signal permission](https://github.com/jaysonguglietta/artexV2/actions/runs/44534830102)
after accepting an enforcing stacked label. This is an operational failure of
that process boundary, not proof that every enforcing stack is unconfined.
The revised bootstrap requires one exact `docker-default (enforce)` profile.
Temporary bootstrap-only `MAC_OVERRIDE` lets the profile transition happen on
first exec into `setpriv`, which then removes all five capability sets, drops to
UID 70, clears groups and retains no-new-privileges before database code. It does
not add `MAC_ADMIN`, `CAP_KILL` or global signal/AppArmor/sysctl exceptions.
[PostgreSQL operations](POSTGRES-BACKUP.md) explains the primary-source basis and
tradeoff. Current source `af9eeb56…` passed 11 actual client-boundary checks,
fresh 22-check encrypted restore, 16/7/3 database capacity checks, 5 AppArmor/15
backup unit tests and 869 PostgreSQL/Go cases (one optional skip), plus 13 packages.
Current hosted integration/frontend/unit/security/recon passed; full hosted
worker/Compose/capacity/backup also passed in all-six-job hosted CI at `375b2c3…`,
as did all eight native reviewed-image gates. Real main OIDC/Fulcio/Rekor
verification and eight negative cases also passed at the same exact source.
[Validation](VALIDATION.md) separates
exact source/identity and completed jobs.

### PG-01: Automatic database clients could retain bootstrap authority

**Severity: Medium. Confidence: High.** CWE-250/CWE-269; OWASP security
misconfiguration. This is a confirmed excess-privilege deployment gap, not a
demonstrated client RCE or authentication bypass.

**Evidence and affected paths:** the PostgreSQL service in
[docker-compose.yml](../docker-compose.yml) needs root and mount/identity
capabilities for its fixed filesystem bootstrap. Its earlier `CMD-SHELL`
`pg_isready` healthcheck inherited container-configured root/capability/AppArmor
settings. PID1 dropping privileges changes only its descendants, not a later
Docker-created healthcheck or exec process. Backup SQL/dump/restore/readiness
invocations in [artex-backup.py](../scripts/artex-backup.py) also need explicit
per-exec profile/capability handling; `--user postgres` alone does not prove the
complete capability/profile invariant.

**Attack path and impact:** if an automatic client encounters a native parsing
or command-execution defect while processing hostile database/network content,
retained bootstrap authority increases impact from a PostgreSQL-user failure to
root/mount/device authority in that container. No such exploitable client defect
was demonstrated. Docker/VM administrators are already trusted and can deliberately
exec arbitrary privileged commands; the fix does not claim to restrict them.

**Fixed and verified locally and in hosted CI at `375b2c3…`:**
[bounded-entrypoint.sh](../postgres/bounded-entrypoint.sh) now shares `exec_confined`
and the guarded `--run-client` branch with the server boundary. The Compose
healthcheck is fixed `CMD /usr/local/bin/artex-postgres-entrypoint --healthcheck`.
Backup `pg_command`, table COPY, dump, readiness and restore use the fixed
`--client`/`--healthcheck` launcher; [container password recovery](../reset-password.sh)
does too. The launcher enters the single enforcing profile before `setpriv`,
then the common guard requires UID 70, cleared groups, all five capability sets
zero and `NoNewPrivs=1`. Only afterward may a known absolute client or `/bin/sh`
run. SQL/client output is never interpreted by the privileged stage. This fixes
the source routing gap. The actual 11-check client proof verified a real Docker
healthcheck and blocked live `psql` process, not just a launcher/config string.
It confirmed UID/GID 70, all five capability sets zero, no-new-privileges and the
single enforcing label, plus real SQL completion and guarded-branch bypass refusal.
The fresh encrypted backup/restore suite passed all 22 with current watched hashes.

**Validation and repeat-test guidance:** inspect live server, supervisor, readiness/healthcheck
and backup client processes independently; verify exact AppArmor label, UID,
all capability sets and no-new-privileges. Exercise actual authenticated SQL,
dump/restore, initialization `pg_ctl` signaling, graceful stop/restart and refusal
of unconfined/stacked/complain/retained-capability paths. Run the bounded backup
integration on new images and both native hosted architectures, preserving source
volumes. A string/config unit test alone cannot close this deployment gap.

## Current native image proof and remaining hosted scope

[Run44540981464](https://github.com/jaysonguglietta/artexV2/actions/runs/44540981464)
passed all eight native reviewed images at
`375b2c3f460d30cddfecd3437e47b543ec5b9b66`, including final PostgreSQL/client
source. Both summaries and every image are `reviewed-findings`: zero blocking,
94 exact reviews per platform, raw85Medium/9Low per worker and zero findings in
other components. This accepts the documented contained residual risk; it does
not repair packages. [Current evidence](../security/image-followup-evidence.json)
retains all eight config/rootfs identities, scanner/DB/policy identity, full raw
findings/reviews and 16 verified report/SBOM hashes. Older PostgreSQL `2f4821…`
and local/standard/strict results are separate historical checkpoints.

[Current Go run44540981490](https://github.com/jaysonguglietta/artexV2/actions/runs/44540981490)
passed **all six jobs**, including client11/tools6/worker53/Compose17/disk16/
migration7/backup22. [Runtime evidence](../security/postgres-runtime-evidence.json)
retains all six downloaded check/hash/source-only JSON reports; all pass results
and source bindings were verified. Documentation run44540981537 passed at the
same source. `main` was fast-forwarded without force to tested `375b2c3…`.
The earlier `af9eeb56…` passed client11/tools6/worker53/Compose17
before failing an overly compressible growth test. Its replacement uses 128 MiB
inline payload and explicit ENOSPC/SQLSTATE53100 and passed all 16 locally.
The [real main OIDC run44542433712](https://github.com/jaysonguglietta/artexV2/actions/runs/44542433712)
also passed, attempt1 at `375b2c3…`: actual keyless blob/image verification and
eight identity/tamper negatives. Production jobs were skipped; no release/tag/
package/enabling variable was created. The source archive has a separate
credential/byte-parity verification receipt; a passed rehearsal is not production publication.

## Earlier November 19 operational follow-up

The four authorized workstreams now have concrete implementation and scoped local
proof: image remediation/stricter publication policy, actual hosted tag protection,
bounded PostgreSQL/log controls and authenticated encrypted backup/restore. These
do not implement the nine-item [product backlog](PRODUCT-READINESS-REVIEW.md).
The original audit findings and November 18 checkpoints remain historical.

- PostgreSQL/Go integration passed **869 cases**, with one optional provider skip
  and 13 additional tested suites. Isolation race, vet and Windows build passed.
- Physical capacity passed **16 disk-full + 7 migration + 3 reserve** checks. The
  exact-source backup/Compose exercise passed **22/22**, plus **15 backup unit,
  8 crypto and 4 log-policy** regressions. See [database/log/backup operations](POSTGRES-BACKUP.md).
- Two actual [tag rulesets](RELEASE-TAG-PROTECTION.md) are active: personal-owner
  creation, and update/deletion restrictions with no bypass. Seven policy tests,
  15 signing tests and 15 publication tests passed. No production tag was created.
- Image-policy regressions passed **26**. Standard CI
  blocks fixable Medium/Low plus active/unverified High/Critical/Unknown. The
  newly authorized `--require-reviewed-findings` publication mode additionally
  blocks every unreviewed active record. Only exact, expiring contained worker
  Ubuntu OS vendor-unfixed Medium/Low reviews are eligible. Mode-boundary tests
  passed; the actual final ARM64 four-image reviewed gate passed with zero blocking.
- Explicit [npm](../tools/npm/README.md) and [pip](../tools/python/README.md) forks
  each passed 3 static and 10 installed source-runtime checks. They change actual
  dependency/code bytes, retain honest local versions and preserve inventory.

The actual final ARM64 **reviewed-findings** image gate passed with **zero
blocking and 94 exact reviews applied**. Worker raw inventory still has **85
Medium + 9 Low active records**, all without a listed fixed version; the other
three images report zero at every severity. This is authorized contained residual
risk, not a patch, zero-active claim or guarantee of non-exploitability.
[Full residual evidence](../security/image-followup-evidence.json) retains every
active finding/review, exact image identity and authenticated DB/report/SBOM hashes.

The final rebuilt images passed **53/53 live worker checks**, **6/6 actual tool/
browser checks** and the **22-check encrypted backup/restore integration**. The
latest isolation race run passed in **8.176 seconds**, including concurrent
close/removal and adjacent cleanup-failure behavior. Callers wait for acknowledged
removal or confirmed absence; a Docker removal conflict is not success. The
backup report and seven watched source hashes matched at that earlier checkpoint;
new PostgreSQL/client edits require another restore validation. The separate
17-check Compose checkpoint, host log activation/rotation and embedded frontend
build also passed with their recorded scope.

The current 188 reviews cover 94 exact records per architecture, with matching
severity and `fixed_version: null`. ARM64 review: `2026-11-19T13:04:35.549172Z`;
amd64 review: `2026-11-19T13:19:24.496638+00:00`, after actual native inventory
verification. All expire `2026-12-19T12:00:00Z`. High/Critical/Unknown and
fixable findings cannot be waived. The older strict diagnostic's 94-record
refusal remains historical proof, not the current reviewed publication criterion.

Initial hosted native amd64/arm64 standard image scans passed, with identical
94 worker OS identities/versions per platform and all 16 report/SBOM hashes
verified. The separate Go run failed PostgreSQL startup in integration and Compose;
worker53/tool6 and frontend/unit/security/recon passed. Full hosted CI is not green.

**At that earlier checkpoint, pending:** PostgreSQL diagnosis, reviewed-mode/OIDC
and source scan. The current `375b2c3…` Go/images/docs/main OIDC checks above
supersede those implementation concerns; a fresh post-edit credential snapshot
is still required. A prior 953-file source snapshot had
six individually reviewed synthetic matches and zero unresolved; it is not the
fresh updated-source scan. [Validation](VALIDATION.md) records the checkpoint
hash, tested implementation `a2b0c63…` and all current identities. The validation
branch was pushed while `main` remained unchanged. No enabling variable, version
tag or production release was authorized or created by these checks.

## Architecture and threat model

The Go API authenticates the operator, calls configured model providers, schedules
agents and owns PostgreSQL records, approval state and signing keys. A separate
trusted broker controls a dedicated Docker daemon. Executable shell, filesystem,
custom command/Python/HTTP, interactive-shell and MCP tools run in separate
containers per agent run. The minimal API image keeps only its static Go service
and required certificate/time-zone/skill data; it carries no assessment toolchain.
The browser uses the API on the same origin.

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
| Mutable update/build inputs could compromise a personal fork | Medium / High | Production in-process apply/rollback is permanently refused. Exact-identity signed release verification and immutable-image/SBOM gates are implemented; hosted OIDC/publication and final integrated validation remain separate checkpoints. See [signed releases](SIGNED-RELEASES.md) and [image security](IMAGE-SECURITY.md). |

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

### Evidence storage and retention

Findings, captures and exports can contain personal data, target credentials or
sensitive business content. [Evidence storage](EVIDENCE-STORAGE.md) describes the
new independent file-admission budgets: evidence 2 GiB, traffic 5 GiB and workspace
5 GiB by default, with entry/body/exchange limits and fail-closed admission.
These do not themselves cap PostgreSQL/WAL, process logs, backups, Docker layers
or total VM storage. Separate [PostgreSQL/log/backup controls](POSTGRES-BACKUP.md)
now enforce a preallocated ext4 database boundary and finite per-container log
rotation, with explicit migration and recovery reserve. Docker/journal policy
was activated on the reviewed VM by restarting both services; a synthetic stdout
fixture proved rotation and final-record retention. Other deployments must
activate their own policy, and existing unrelated containers require recreation.
Backup destination and total VM capacity remain separate responsibilities.

Raw captures have a 30-day retention policy. Legal holds apply to a whole storage
bucket and block destructive operations/retention until deliberately released;
they are not a claim of immutable or forensic-atomic storage. Define assessment
and backup requirements before applying a hold or deleting evidence.

Raw SDK debug transcripts default off for ordinary binaries. Compose explicitly
enables bounded ephemeral tmpfs; live debug files are excluded from workspace
legal holds and disappear on restart. They are not ordinary persistent raw files.
PostgreSQL UI conversation/activity history remains separate. Model-context replay
and output handling are covered by the fresh PostgreSQL/race runs; their behavior
is not an independent evidence or deletion certification.
Keep runtime data/backups private and limit external provider retention. Central
append-only audit remains separate work. Encrypted/authenticated backups now
receive an independent bounded restore with schema/table/file comparisons; the
22-check exact-source integration passed. Full human-run VM recovery, measured
RPO/RTO and deletion of every external/backup copy are not established by that test.

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

Earlier official govulncheck v1.10.0 reported no findings in its checked default
Go source graph (52 modules; 20 root packages). Its recorded database was last modified on
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

### Runtime image and release controls

[Image security](IMAGE-SECURITY.md) now supplies OS/language inventories, complete
Trivy JSON and CycloneDX SBOMs for exact IDs/platforms, a fresh bounded DB policy,
EOL/inventory checks and strict active/unverified High/Critical/Unknown gating.
Standard CI also blocks fixable Medium/Low. Signed publication uses
`--require-reviewed-findings` and permits only exact contained vendor-unfixed
worker Ubuntu OS Medium/Low reviews with severity/platform/version binding, a
primary Ubuntu reference and expiry within 30 days. Every unreviewed active record
blocks; High/Critical/Unknown and fixable findings cannot be waived. The independent
strict diagnostic still blocks all active records. Withdrawn classifications
require separate fresh primary proof and are not accepted active risks.
Earlier November 18 local/hosted passes and raw counts remain in
[validation](VALIDATION.md), bound to those exact source/image/DB identities.
The actual current ARM64 reviewed-mode result is recorded above, with all 94
active worker records and their reviews visible. Initial native amd64/arm64 standard scans passed, while the new reviewed-mode
hosted reviewed-mode run passed at `375b2c3…`; there is no all-CVE clean or
zero-active claim.

Production cannot enable the retained unsigned updater: apply/rollback routes
always refuse, the legacy flag is rejected, and production bootstrap/staging does
not execute old unsigned files. Version checks remain read-only. The
[signed-release workflow](SIGNED-RELEASES.md) is restricted to protected semantic
tags and explicit opt-in, with exact repository/workflow/ref/commit/run identities
and verification before installation. Main-only dry runs use real OIDC without
publishing. Current local signing/publication counts are recorded above. The
November 18 real hosted OIDC rehearsal passed its positive verification and eight negative cases;
[validation](VALIDATION.md) links its exact commit/run. Production-tag release
publication remains a separate opt-in operation and is not claimed.

## November 18 validation checkpoints

| Check | Recorded result |
|---|---|
| Earlier PostgreSQL integration checkpoint | 817 test cases passed, including subtests; 1 optional live-provider test skipped without external credentials |
| Earlier live-worker checkpoint | 52 passed against local disposable Docker targets |
| Earlier container API smoke checkpoint | 12 passed |
| Frontend tests at checkpoint | 26 passed across 8 files, including disabled/missing update capability and clipboard-failure regressions |
| Frontend TypeScript and English catalog | Passed, including a clean source-only TypeScript check |
| Earlier frontend static build/export checkpoint | Passed; 30 HTML pages checked at that checkpoint. Fresh source-image builds also passed with the current static frontend |
| Biome | 0 errors; 124 warnings and 659 informational diagnostics retained |
| Earlier Go analysis/portability checkpoint | `go vet ./...` and Windows cross-build passed before the latest storage/release changes |
| Image-gate regressions at checkpoint | 19 passed, including primary withdrawal, runtime inventory and OCI/config/layer identity tests |
| Signing/publication regressions at checkpoint | 14 signing + 15 publication tests passed; not hosted OIDC verification |
| PostgreSQL/Go integration at checkpoint | 869 cases passed, 0 failed, 1 optional live-provider skipped; 13 additional tested suites passed |
| Races and analysis at checkpoint | storage/traffic/isolation/evidence/agent/server races, `go vet ./...` and windows/amd64 API cross-build passed |
| Builds and live boundary checks at checkpoint | All four then-current source-image builds, worker 52/52 and API 12/12 passed |
| ARM64 image gates at checkpoint | All four passed, 0 blocking; raw withdrawn High and Medium/Low counts retained |
| Compose acceptance at checkpoint | 11/11 passed, including PostgreSQL native UID drop/restart persistence, absent API interpreters, isolated tool execution and cleanup |
| Hosted checks | All six Go/worker jobs, frontend, docs and detections passed; all eight amd64/arm64 images passed; real keyless signing and eight negative cases passed. Exact runs in validation. |
| Production-tag release | Not published; protected-tag settings and explicit release opt-in remain operator policy. |
| Dependency and source-secret review | Results and limits described above and in the validation record |

This entire table is the November 18 checkpoint, including rows then labeled
current/fresh. The new November 19 source has separate results and pending checks
above and in [validation](VALIDATION.md). These results
demonstrate checked behaviors, not a security certification.
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
python3 scripts/test-image-security.py
python3 scripts/test-signed-release.py
python3 scripts/test-release-publication.py
python3 scripts/test-release-tag-policy.py
python3 scripts/test-backup.py
python3 scripts/test-backup-crypto.py
python3 scripts/test-log-policy.py
python3 scripts/test-npm-fork.py
python3 scripts/test-pip-fork.py
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
2. Complete final ID-bound scans and new hosted native-architecture/OIDC checks
   after the current changes. Enforce exact, expiring reviewed-findings publication
   policy; never extend it to fixable or High/Critical/Unknown findings. Active
   tag rules and prior OIDC evidence do not eliminate upstream compromise
   or establish an actual version-tag publication.
3. Repeat the now-verified host log activation/rotation on each actual deployment,
   preserve explicit database migration/recovery procedures, and schedule verified
   encrypted backups with separate retention/capacity. Extend disposable restore
   proof to a measured human-run VM recovery. Central append-only audit remains
   separate; no total-host quota or universal deletion guarantee is claimed.
4. Design a bounded distributed traffic-capture/import pipeline if required.
   Workers cannot use the API recording proxy, privileged raw-packet scans are
   unsupported, and oversized unsynchronized files remain ephemeral.
5. Re-run boundary regressions, secret/advisory scans and measured model behavior
   checks after SDK, deployment or prompt changes. Preserve original evidence and
   technical compatibility data rather than rewriting them for a clean text scan.

[Secure deployment](SECURE-DEPLOYMENT.md), [English conversion](ENGLISH-CONVERSION.md)
and [source upload](../UPLOAD.md) describe the corresponding operator workflows.

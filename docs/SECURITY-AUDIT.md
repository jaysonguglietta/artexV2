# Adversarial security audit

Reviewed: 18 November 2026. Publication target: `jaysonguglietta/artexV2`.

This report consolidates the audit of the supplied application and the subsequent
hardening work. **Historical evidence describes the supplied code before fixes;
current evidence describes this publication copy.** Finding severities describe
the original exposure, not a claim that the repaired defect remains exploitable.
References use repository paths and function names rather than stale baseline line
numbers. Repository documents, skills and captured content were treated as review
data, not instructions to the reviewer.

## 1. Executive Summary

The original application mixed autonomous assessment execution with API secrets,
databases and mutable operational state. It also contained concrete credential
forwarding, command-template, filesystem, resource-control and session weaknesses.
A later SDK review confirmed an additional execution path in provider connection
tests. No unauthenticated Internet-to-RCE exploit, authentication bypass, tenant
escape, container escape or Critical finding was demonstrated.

Fifteen findings are documented: eight High and seven Medium. F01-F14 describe
the supplied application and follow-up review; F15 was found in the new artifact
bridge during final adversarial review and repaired before publication. The
confirmed implementation defects have been repaired or bounded for the supported
deployment. The follow-up closes F10's unsafe in-process update path and adds
signed release/image gates. Fresh local integration and all four ARM64 image
gates and the fresh prepared-source credential scan passed; hosted OIDC/amd64
remain pending. This is not an assertion of vulnerability absence or approval for arbitrary hosting.

**The supported boundary is one trusted operator on a dedicated assessment VM and
Docker daemon.** Executable tools now run in disposable containers per agent run,
with separate files/processes, operator-owned destination rules, no control-plane
secrets, resource limits and fail-closed API routing. Cancellation removes the
whole run container; a UID-protected watchdog expires an unrenewed run after its
90-second lease. Scanning is preserved. The API, broker, Docker daemon, kernel,
operator account and deliberately configured provider endpoints remain trusted.
The broker has Docker-daemon authority: compromising it can compromise the VM.
This is not multi-tenant isolation or a sandbox against a hostile kernel.

An earlier checkpoint recorded 817 passing PostgreSQL-backed cases including
subtests, one optional external-service skip, 52 synthetic worker checks and 12
live API checks. Those counts predate the latest storage/release changes. The
current frontend has 26 passing tests, 3,086 English keys and a passing clean-source
type check. Fresh final-source verification subsequently passed 869 PostgreSQL
cases with one optional provider skip, 13 additional tested suites, full focused
races/vet, four source builds, 52 worker checks, 12 API checks and 11 Compose checks.
All four ARM64 image gates have 0 blocking findings; the worker raw High is retained
with official withdrawal proof. The fresh prepared-source credential scan has no
unresolved matches; hosted OIDC/amd64 remain pending. Exact counts/identities are in [validation](VALIDATION.md).
The latest complete frontend npm audit and production audit both report zero;
Go source `govulncheck` reports zero. Earlier npm graph alerts were independently
verified as withdrawn, as explained in section 7. See [validation](VALIDATION.md)
for commands, scope and limitations.

Signed provenance verification, exact-image vulnerability/SBOM gates and scoped
evidence quotas/retention/holds are now implemented. Priorities are completing
their real hosted and final-source checks, operating attributable external audit
storage, testing backup/restore/deletion and obtaining an independent repeat
assessment. Shared untrusted users require a separate identity,
authorization and tenancy design before deployment.

## 2. System Overview and Trust Boundaries

### Architecture and assets

The Go entrypoint [cmd/artex/main.go](../cmd/artex/main.go) starts the HTTP API,
manager, task orchestration and optional loopback traffic-recording proxy. A
Next.js/React interface is statically exported for the production image. PostgreSQL
holds settings, authentication state, profiles, tasks, assets and findings; SQLite
and bounded filesystem artifacts hold additional traffic/evidence/runtime data.
The external Norma SDK supplies agent sessions and tool implementations.

The minimal API image contains the static Go service and required certificates,
time-zone data and skills, without Node/Python/browser assessment tools. The API
performs authentication, model/search requests, metadata operations and audit
bookkeeping. Authenticated worker requests go to
[isolation/broker.go](../isolation/broker.go). The trusted broker creates a separate
container for each `(scope, run_id)`; task scopes select an operator-authored CIDR
policy. Inside it, [isolation/runtime.go](../isolation/runtime.go) runs SDK,
interactive-shell, custom command/Python/HTTP and MCP tools. Runtime credentials
are limited to explicitly granted integration capabilities. Task files move as
bounded regular-file snapshots, not shared host mounts.

Assets include operator sessions/password hashes; JWT signing material; database,
model/search and integration credentials; target traffic and credentials; findings,
reports and uploaded evidence; interception keys; task/scope/approval rules; the
Docker broker token and replay ledger; source, build inputs and release artifacts.

| Boundary | Untrusted or sensitive material | Current enforcement / remaining trust |
|---|---|---|
| Browser to API | Credentials, cookies, JSON, uploads, IDs | JWT/cookie authentication, same-origin mutation checks, bounded requests; one shared operator principal |
| Target to agent | Pages, tool output, files, protocol messages | Content remains untrusted; prompt/reviewer decisions do not replace OS enforcement |
| API to broker | Authenticated tool requests and trusted run attribution | Private authenticated broker; operator-owned policy; API and broker trusted |
| Broker to worker | Fixed container/exec specification, task files, granted integration config | Immutable worker image ID, dedicated bridge, fixed entrypoint, no image healthcheck, no host mounts |
| Worker to control plane | Attempted secret access, network pivots, escaped processes | No API/DB/JWT/broker credentials or Docker socket; CIDR deny rules; separate UID/process/filesystem namespace |
| Worker to assessment network | Commands, browser/HTTP/MCP requests, DNS | Approved IPv4 destinations; management/Docker/link-local denies take precedence; IPv6 denied except loopback |
| Worker to evidence store | File names/bytes and tool results | `os.Root`, regular-file restrictions and size/count limits; content is not trusted forensic evidence |
| MCP to client | SSE endpoint, response bytes, tool schema/content | Same-origin handoff, no redirects, bounded parsing, verified TLS when TLS is used; integration destination/credential deliberately granted |
| Build/update system to runtime | Dependencies, images, actions, downloaded code | Lockfiles/pins, exact-ID image/SBOM gates and exact-identity external signature verification; production in-process mutation is refused; hosted release settings and final validation remain explicit |

### Supported execution limits

Each run has a read-only image, a 512 MiB workspace tmpfs, a separate bounded
`/tmp`, 1 GiB memory, one CPU and a 128-process limit. The daemon and fresh RPC
clients use UID/GID 10001, no-new-privileges, and empty inheritable, permitted,
effective, bounding and ambient capability sets. The separate root-identity PID1
watchdog also has no capabilities. Only a fixed broker root helper can renew it;
UID 10001 tasks cannot signal it or impersonate renewal.

Normal RPC admission is bounded and serialized; background work can proceed in
sibling containers. Close/lease requests have independent small-body admission.
The private append/fsync ledger remembers up to 100,000 admitted identities and
rejects replay after close or restart; corruption/exhaustion refuses new admission.
Broker startup removes its owner-labeled orphan containers. No production local
execution fallback or host-execution opt-in remains. See
[worker isolation](WORKER-ISOLATION.md) for the exact setup and limitations.

## 3. Threat Model

| Attacker | Capabilities and objective | Realistic path and assumptions |
|---|---|---|
| Hostile assessment target | Controls pages, redirects, responses and downloaded bytes | Induce unsafe model actions or resource consumption; model obedience is conditional, not assumed proven |
| Compromised MCP service | Controls SSE handoff, schemas, tool content and stream timing | Redistribute configured credentials, request other destinations, exhaust clients, or influence agent actions |
| Compromised model endpoint | Returns arbitrary provider responses/tool-use messages | Historical F14 executed local defaults during a connection probe; current probe offers no tools |
| Unauthenticated reachable client | Reaches login/setup and public health | Body/connection/bcrypt exhaustion; no demonstrated setup takeover without the setup secret |
| Stolen operator session | Uses intended administration and task/tool APIs | Broad operator compromise; no lesser role is established from which to claim privilege escalation |
| Compromised task tool | Arbitrary native code as worker UID10001 | Try secrets, Docker socket, sibling files/processes, network policy, detached persistence or watchdog renewal |
| Compromised local/container neighbor | Network adjacency and its own identity | Historical open recording proxy, cleartext internal transport, or accidentally shared mounts; no host-root privilege assumed |
| Compromised publisher/dependency | Changes package, action or release artifacts | Build/update execution within explicitly trusted release channels; no actual publisher compromise claimed |

Important combined paths were reviewed: remote content to command parameters;
MCP origin handoff to credential forwarding; symlinks to file APIs; model tool
responses to SDK defaults; policy-read failure to unauthorized writes; and detached
processes outliving API cancellation. Root/operator/API/broker/daemon compromise is
outside the worker guarantee. An operator may deliberately grant a broad target
range or integration key; this remains a powerful authorization decision.

## 4. Attack Surface Inventory

| Component | Entry points and attacker-controlled inputs | Review focus |
|---|---|---|
| Authentication/session | `server/auth.go`, `security.go`; login/init/change/logout, JWT/cookie/header/body | Setup race, bcrypt, strict claims, DB errors, revocation, origins, body/concurrency bounds |
| Authorization/business logic | `server/server.go`, task/asset/visibility handlers, `agent/tools_insert.go`, `db/` | Trusted IDs, policy errors, administrative authority, task lifetimes and partial writes |
| File ingestion/access | `workspace.go`, `chatupload.go`, `staged_uploads.go`, `skill_zip.go`, task archive handlers | Traversal, symlinks, special files, ZIP/count/byte limits, staged-upload ownership and rollback |
| Agent/tool execution | `agent/`, `server/customtool.go`, `isolation/`, worker commands | SDK defaults/late injection, argv boundaries, credentials, cancellation, cgroups, replay and resource limits |
| Integrations/outbound | `mcphttp/`, `mcpdiscover.go`, `sync_scopesentry.go`, recon helper, notification/search/provider clients | Origin binding, redirects, TLS, explicit destinations/credentials, output/time limits |
| Recording/evidence | `traffic/`, enrichment helpers, logs/SSE/export/blob APIs | Proxy exposure, TLS, query construction, sensitive content, storage and streaming lifetimes |
| Frontend | `web/src/lib/`, components, URL/Markdown/evidence rendering, Next config | Token storage, URL credentials, origin handling, unsafe rendering and production/development differences |
| Delivery/deployment | Docker/Compose, install/update/build scripts, `selfupdate/`, workflows | Build trust, permissions, secret/state mounts, immutable pins, update identity and publication contents |

The initial audit inventoried 794 regular files in the supplied archive. Review
used component tracing and repository-wide sink/pattern searches, followed by
focused regression tests and real synthetic runtime checks. Enumeration is not a
claim that every line received equivalent manual review. SQL/query and frontend
rendering sinks were searched/sampled; no confirmed SQL/NoSQL/LDAP injection,
template RCE, unsafe object-deserialization exploit or stored XSS is asserted.
That does not prove these classes absent. Notification destinations, provider
privacy contracts and external services need deployment-specific review.

## 5. Prioritized Findings

The table retains original severities and IDs. F14 is listed near the top because
it bypassed the intended worker boundary, despite being discovered later.

| ID | Original severity | Confidence | Current disposition |
|---|---|---|---|
| F14 | High | High | Fixed: connection probe has explicit empty tools and no background injection |
| F01 | High | High | Fixed: MCP handoff origin and redirect restrictions; isolated execution |
| F02 | High | High | Fixed: command tools use fixed executable and literal argv |
| F03 | High | High | Fixed in reviewed workspace/skill/synchronization operations |
| F04 | High | High for boundary defect | Remediated for documented dedicated-VM/single-operator boundary |
| F05 | High | High | Fixed for supported loopback recording-proxy topology |
| F06 | High | High | Bounded request/body/concurrency paths; ingress-scale DoS remains operational |
| F13 | High | High | Fixed helper defaults/origin/redirect/output controls plus worker egress |
| F07 | Medium | High | Fixed browser/query session handling and durable logout revocation |
| F08 | Medium | High | TLS verification restored by default; explicit assessment override remains |
| F09 | Medium | High | Reviewed parsing/output and new file-admission budgets bounded; database/log/VM-wide capacity remains separate |
| F15 | Medium | High | Fixed: whole-map path/depth/directory validation before mutation |
| F10 | Medium | High | Unsafe production updater closed; signed release/image controls implemented; hosted OIDC/publication verification pending |
| F11 | Medium | High | Reviewer failures deny by default; explicit legacy allow policy requires review |
| F12 | Medium | High | Policy read errors refuse insertion before side effects |

### F14 — Provider connection probe restored executable SDK defaults

**Severity: High. Confidence: High. Classification: confirmed execution defect;
fixed. CWE-749 / CWE-862; OWASP excessive agency.**

**Affected files/functions:** [agent/provider.go](../agent/provider.go),
`TestConnection`; `server/server.go::testLLM`; SDK `agentcore.NewSession` defaults.

**Description and historical evidence:** the connection test called
`agentcore.Run` with bypass permissions and an omitted/nil `Tools` slice. Norma
v0.4.4 interprets nil as `tool.DefaultTools()`, including local shell/filesystem
tools. Testing a provider should authorize only a model response. A system prompt
asking for `OK` does not constrain a hostile provider's structured tool response.
A synthetic endpoint reproduced the old behavior using an owned temporary canary.

**Exploitation scenario / prerequisites:** an administrator tests a malicious or
compromised model endpoint, which responds with a Bash tool-use message. The SDK
could execute it in the API process. This required provider control or compromise,
not an unauthenticated request to ARTEX.

**Potential impact:** API-readable service credentials, signing/data files and
network authority could be compromised independently of isolated task execution.

**Fix and patch guidance:** current options explicitly set
`Tools: []actool.CoreTool{}` and `DisableBackgroundTasks: true`. Keep the probe
separate from normal agent sessions; do not use nil to mean no capabilities. Review
SDK default/late-injection behavior on every upgrade. Production session creation
also normalizes nil tools and routes executable late additions remotely.

**Validation:**
[agent/connection_security_test.go](../agent/connection_security_test.go),
`TestConnectionProbeNeverOffersOrExecutesTools`, sends a real SDK-shaped malicious
tool response, asserts no tools were advertised, no canary exists, and a tool-only
reply is not a successful probe. Session late-injection tests independently cover
Monitor/WebFetch and nil tools.

### F01 — Legacy MCP SSE handoff forwarded credentials to another origin

**Severity: High. Confidence: High. Classification: confirmed data flow; fixed.
CWE-918 / CWE-201; OWASP SSRF.**

**Affected files/functions:** [mcphttp/client.go](../mcphttp/client.go), `NewSSE`,
`readSSEEndpoint`, `legacyRoundTrip`; `server/mcpdiscover.go::connectMCP`.

**Description and historical evidence:** the server-announced SSE endpoint became
`messageURL` without origin/scheme restrictions. The subsequent JSON-RPC POST
copied configured headers, including Authorization/API keys, to that URL. Absolute,
protocol-relative or downgraded destinations could replace the configured origin.

**Exploitation scenario / prerequisites:** a configured malicious/compromised MCP
service announces an external collector or a service-local URL. Initialization
sends a constrained MCP JSON-RPC POST and configured headers there before normal
tool use. Cloud metadata credential theft was not demonstrated from this POST-only
primitive.

**Potential impact:** credential redistribution/plaintext exposure, internal
requests and compatible-service pivots, conditional on destination reachability.

**Fix and patch guidance:** current `resolveMessageEndpoint` resolves relative
URLs but requires the configured scheme/host, rejects userinfo/fragments and permits
only HTTP(S). `remoteHTTPClient` rejects redirects. Keep the invariant
`resolved.Scheme == base.Scheme && resolved.Host == base.Host` under canonical
comparison. Prefer HTTPS for credentialed integrations; an explicitly configured
plain HTTP destination remains an operator trust decision. Production MCP runs in
the isolated worker and must also pass its network policy.

**Validation:** [mcphttp/security_test.go](../mcphttp/security_test.go) covers origin
binding, cross-origin refusal before credential transmission, bounded streams and
canceled handshake; same-origin legacy SSE tests retain legitimate behavior.
Recheck scheme downgrade, alternate port and redirect variants after client changes.

### F02 — Quoted command-template placeholders allowed shell injection

**Severity: High. Confidence: High. Classification: confirmed primitive with
configuration-dependent reachability; fixed. CWE-78; OWASP injection.**

**Affected files/functions:** [server/customtool.go](../server/customtool.go),
`runCommandTool`, former shell quoting/substitution, current `commandWords` and
`renderTemplate`.

**Description and historical evidence:** individually single-quoted values were
inserted into arbitrary shell source. A template such as `printf "%s" "{target}"`
placed those quotes inside existing double quotes, allowing command substitution
inside a supposedly data-only parameter. Single-pass shell escaping cannot safely
repair every surrounding shell grammar context.

**Exploitation scenario / prerequisites:** an operator configures such a quoted
template and target-derived/model-controlled text reaches the placeholder. Text
containing a command substitution executes commands. No claim is made that every
bundled tool had a vulnerable configured template.

**Potential impact:** unintended arbitrary runner commands; historically F04
amplified their effect to API secrets and persistent state.

**Fix and patch guidance:** current command tools use
`exec.CommandContext(ctx, fixedProgram, args...)`; each argument is interpolated as
literal data. Legacy command strings are parsed into words and reject shell
operators. Executable selection is not a parameter placeholder. Use a fixed program
plus argument array in new specifications, substitute once, and add `--` before
user data when the selected utility supports it. Explicit Bash/script tools still
intentionally execute code inside the worker.

**Validation:** [server/execution_security_test.go](../server/execution_security_test.go),
`TestCommandArgumentsStayLiteral`, runs quoted/unquoted templates with substitution,
quote and metacharacter payloads, rejects shell operators and prevents recursive
placeholder expansion. Worker checks independently verify the execution boundary.

### F03 — Lexical filesystem checks allowed escaping symlinks

**Severity: High. Confidence: High. Classification: confirmed confinement defect
with symlink prerequisite; fixed in reviewed paths. CWE-59 / CWE-22; OWASP broken
access control.**

**Affected files/functions:** [server/workspace.go](../server/workspace.go),
workspace read/write/upload/download/delete; `server/server_mgmt.go` skill CRUD;
`server/platform_tools.go::toolCreateSkill` / `toolUpdateSkillFile`; artifact
synchronization in `isolation/files.go`.

**Description and historical evidence:** paths passed lexical prefix checks, then
ordinary filesystem opens followed a symlink outside the intended root. The same
pattern also existed in agent-facing skill editing, beyond HTTP file handlers.

**Exploitation scenario / prerequisites:** an actor capable of creating a
workspace/skill symlink leaves a link to an outside directory. An authenticated
read/download follows it; writes/uploads can modify outside files. Upload alone
was not proven to create symlinks, and no ZIP-slip exploit was demonstrated.

**Potential impact:** outside reads/writes within the service identity's access.
An already unrestricted administrator shell had equivalent authority; that does
not make the advertised file boundary correct. No Docker host-root escape is
claimed.

**Fix and patch guidance:** current reviewed operations open `os.Root` and perform
root-relative operations, with regular-file and size restrictions where read data
is exported. Download from the safely opened descriptor. Preserve this pattern;
`EvalSymlinks` followed by an ordinary open reintroduces a replacement race.

**Validation:** [server/files_security_test.go](../server/files_security_test.go)
checks read/download/write/delete/mkdir against unchanged outside canaries, plus
normal nested behavior. Platform skill regressions cover root/parent links and
normal create/update. Isolation artifact tests cover traversal, symlink escapes,
byte budgets and concurrent UI-file preservation. Continue race/special-file tests
when adding file APIs.

### F04 — Autonomous execution shared API privileges and secrets

**Severity: High. Confidence: High for the architecture defect; Medium for a
malicious-target-to-model-compromise chain. Classification: confirmed design gap;
remediated for the supported boundary. CWE-250 / CWE-269 / CWE-863.**

**Affected files/functions:** agent chat/main/planner/worker session setup;
`server/customtool.go` executors; MCP discovery and ScopeSentry integration;
Docker/deployment configuration. Current controls are in
[agent/isolated_session.go](../agent/isolated_session.go),
[isolation/](../isolation/), [cmd/tool-worker/](../cmd/tool-worker/),
[worker/entrypoint.sh](../worker/entrypoint.sh) and
[compose.workers.yml](../compose.workers.yml).

**Description and historical evidence:** bypass-mode execution and custom Python
ran as the API identity; Python inherited the service environment. The supplied
container had no nonroot `USER`, independent execution service or task network
boundary. Optional model review/guard rules were not process isolation.

**Exploitation scenario / prerequisites:** malicious target/tool content induces
an allowed code action, or an allowed tool is compromised. Historically that code
could access API-readable credentials, PostgreSQL, other tasks and writable skills.
Successful prompt injection was not assumed or demonstrated for every model.

**Potential impact:** control-plane secret/data compromise, cross-task access and
persistent mutation. Container root did not automatically imply host root.

**Fix and patch guidance / current evidence:** trusted orchestration assigns a
random per-run identity before tool assembly. SDK/default/late-injected execution,
custom HTTP/command/Python, interactive shells and MCP are routed to per-run
containers; failure never falls back locally. Workers have private tmpfs and no
API/DB/JWT/broker/model secrets, host mounts or Docker socket. A broker-authored
allowlist, with preceding management/Docker/link-local denies, governs all child
network access. The broker rejects host/shared/default network modes and fixes the
entrypoint and privilege drop for every fresh exec client. Session completion and
cancellation use urgent close outside the scan gate; the replay ledger prevents
reopening. Removing the container stops setsid/double-fork descendants, and the
independent watchdog bounds orphan execution during control-plane failure.

Preserve these independent controls in future tool integrations. Do not add a
host-execution flag, shared live task mount, API proxy tunnel, general broker
command API or automatic secret inheritance. Grant integration credentials narrowly
and keep the broker/token/state restricted to its dedicated VM management boundary.

**Validation:** isolation/session/MCP lifecycle and replay/ledger tests plus 52
live synthetic checks in [scripts/test-workers.py](../scripts/test-workers.py).
They exercise actual exec-client capabilities, absent secrets/socket mounts,
approved/denied destinations, private sibling files, urgent foreground cancellation,
double-fork network termination, sibling survival, watchdog expiry, snapshots and
broker-crash restart replay rejection. Passing these is evidence for the tested
boundary, not proof against kernel/Docker/broker compromise. Atomic forensic
snapshots, privileged raw-packet scans and distributed proxy recording are not
provided.

### F05 — Recording proxy lacked a container-network access boundary

**Severity: High. Confidence: High; exposure topology-dependent. Classification:
confirmed configuration defect; fixed for supported deployment. CWE-306 / CWE-918.**

**Affected files/functions:** [traffic/traffic.go](../traffic/traffic.go), `Open`;
original Docker command/Compose proxy publication; current CLI/proxy topology.

**Description and historical evidence:** the original container started its
unauthenticated forward proxy on all container interfaces. A loopback host port
mapping did not prevent direct access from another container on the bridge.
API JWT authentication did not protect the distinct proxy protocol.

**Exploitation scenario / prerequisites:** a reachable sibling-container/network
attacker uses HTTP proxy requests or CONNECT through the application network
vantage. Internet access was conditional on additional publication/routing; default
host-loopback publication alone did not establish Internet exposure.

**Potential impact:** unauthorized relay/internal reachability, attribution abuse
and capture-storage consumption.

**Fix and patch guidance:** `traffic.Open` now refuses non-loopback addresses;
CLI/container defaults bind loopback and Compose does not publish the proxy.
Workers cannot reach the API recording proxy. Keep it local; any future distributed
recorder needs authenticated per-worker transport plus independent destination
policy, rather than reopening this listener. Same-namespace local processes remain
part of the trusted API boundary.

**Validation:** [traffic/security_test.go](../traffic/security_test.go),
`TestProxyRejectsPublicListenAddress`, plus deployment/API/worker checks. Verify
actual listener and published-port configuration after every deployment change;
never use CORS as a proxy authentication mechanism.

### F06 — Public authentication allowed unbounded body and bcrypt work

**Severity: High. Confidence: High. Classification: confirmed missing limits;
reviewed paths bounded. CWE-400 / CWE-770; OWASP API resource consumption.**

**Affected files/functions:** `server/auth.go` authentication handlers; historical
shared decoder; [server/security.go](../server/security.go), `securityBoundary`;
`cmd/artex/main.go` HTTP timeouts.

**Description and historical evidence:** JSON decoding had no body cap, the server
only bounded headers, and failure throttling did not reserve concurrent bcrypt
work. A burst could pass the limiter before failures were recorded. Password
length checks did not bound JSON allocation or slow-body connection occupancy.

**Exploitation scenario / prerequisites:** any client reaching login submits large
or slow JSON bodies or concurrent ordinary password attempts. A reverse proxy
might mitigate exposure, but no such enforced assumption repaired the source.

**Potential impact:** unauthenticated CPU/memory/connection exhaustion; no precise
crash threshold was claimed.

**Fix and patch guidance:** the current boundary applies 8 KiB limits and four
concurrent expensive authentication slots before decoding/bcrypt, with 429 on
saturation and body read deadlines. General JSON/upload paths also have explicit
budgets. Keep authentication admission global, retain peer throttling, and preserve
streaming responses rather than imposing a blanket short response deadline. Add
ingress connection/rate budgets appropriate to the actual deployment.

**Validation:** security boundary tests cover body/origin limits and authentication
concurrency; live API checks reject oversized login. Continue controlled slow-body
and distributed-load tests under deployment CPU/memory limits. These controls bound
individual application paths; they do not promise availability against arbitrary
network-volume attacks.

### F13 — Recon helper forwarded untrusted requests and credentials

**Severity: High. Confidence: High. Classification: confirmed helper policy
weakness; fixed with documented network residuals. CWE-918 / CWE-200 / CWE-770.**

**Affected files/functions:**
[skills/api-recon/scripts/runtime_harvest.js](../skills/api-recon/scripts/runtime_harvest.js),
`preload.js`, current `runtime_policy.js::prepareConfig`, `requestAllowed`,
`forwardRead`, and helper dependency lockfile.

**Description and historical evidence:** forwarding defaulted on, API-looking URLs
could select arbitrary destinations, configured cookies were attached without an
origin boundary, fetch followed redirects, and response reads lacked size/time
bounds. Chromium always disabled its sandbox and TLS verification. The helper's
separate dependency tree also required remediation.

**Exploitation scenario / prerequisites:** an operator runs the helper against a
hostile page. It selects a collector or a redirecting same-origin API URL; manual
forwarding can redistribute configured sensitive cookies, issue unintended requests,
or consume worker memory. Synthetic-cookie examples do not establish that every
operator-provided value is nonsensitive.

**Potential impact:** credential exposure, SSRF and worker DoS; no unauthenticated
ARTEX endpoint exploit or cloud credential retrieval was demonstrated.

**Fix and patch guidance:** mock by default. Explicit forwarding is restricted to
same-origin GET/HEAD, no redirects, bounded responses/runtime/counts and no silent
proxy bypass. Do not synthesize cookies for secondary origins. Keep TLS and browser
sandbox enabled unless an explicit disposable-test exception is authorized. Current
helper dependencies are locked and report zero npm findings. Browser observation
hooks are not a complete network sandbox; run the helper only within enforced
worker egress policy and use synthetic credentials for the mock workflow.

**Validation:** `runtime_policy.test.js` covers opt-in, origin/method/redirect/cookie
collector, response/timeout and proxy policy. Synthetic Chromium tests cover mock
and permitted read-forwarding/fetch/XHR behavior. Those browser tests use their
explicit sandbox exception only within a disposable networkless container.

### F07 — Bearer sessions were exposed to JavaScript and URLs

**Severity: Medium. Confidence: High. Classification: confirmed session exposure
amplifier; fixed reviewed handling. CWE-598 / CWE-922.**

**Affected files/functions:** frontend `web/src/lib/auth.ts` / `api.ts`;
[server/auth.go](../server/auth.go), `extractToken`, `signJWT`, `setSessionCookie`,
`authLogout`; session revocation storage.

**Description and historical evidence:** the UI saved the seven-day bearer token
in localStorage and a JavaScript-set cookie, SSE placed it in a query string, and
logout lacked durable server-side revocation. Password changes already invalidated
old password-bound sessions; that protection was retained.

**Exploitation scenario / prerequisites:** an actual same-origin script compromise,
access-log disclosure or insecure transport exposes the token. No independent XSS
or account takeover was demonstrated. Logout would not revoke an already copied
token.

**Potential impact:** replay of a full shared-operator session and broad application
control.

**Fix and patch guidance:** current browser authentication uses server-set HttpOnly,
SameSite Strict cookies, with Secure outside loopback/TLS as appropriate. JWTs have
12-hour expiry and strict algorithm/subject/expiry checks; query tokens are rejected,
browser persistence removed, and logout persists a token digest revocation. Mutation
origin checks accompany cookie auth. Use same-origin credentialed SSE; keep explicit
Bearer API tokens private. HttpOnly reduces token extraction, but does not make
same-origin XSS harmless or authorize plaintext remote deployment.

**Validation:** session tests cover cookie attributes/query rejection and durable
logout revocation; live API checks cover same-origin mutations and revoked-token
reuse. Recheck SSE reconnect and password-change invalidation when modifying auth.

### F08 — Upstream target TLS validation was disabled by default

**Severity: Medium. Confidence: High. Classification: confirmed confidentiality
weakness; secure default restored. CWE-295; OWASP cryptographic failures.**

**Affected files/functions:** `traffic/traffic.go::Open` and
[enrich/enrich.go](../enrich/enrich.go), `buildClient`.

**Description and historical evidence:** proxy `SslInsecure` and enrichment
`InsecureSkipVerify` were unconditionally true. A downstream trusted interception
certificate did not establish authenticity of the upstream assessment target.

**Exploitation scenario / prerequisites:** an on-path actor supplies an untrusted,
wrong-host or expired upstream certificate. The original clients accepted it and
could send sensitive target requests or store forged evidence.

**Potential impact:** target credential interception and evidence manipulation.
Support for deliberately self-signed targets is reasonable; the blanket default was
the defect. No effect on a separately validating updater was assumed.

**Fix and patch guidance:** validation is now on by default; the explicit
`ARTEX_INSECURE_TARGET_TLS=1` assessment override is visible in source. Prefer
assessment-specific CA trust; restrict any insecure override to a disposable
assessment environment and never use it for control-plane secrets. A future
host-specific exception is preferable to a process-wide override. Unused API-side
enrichment startup was removed rather than leaving a dormant target-network path.

**Validation:** inspect both transports and exercise trusted/untrusted/wrong-host
certificates before enabling a new assessment topology. Current package checks
passed; no claim is made that every certificate combination was tested live.
Production MCP rejects its separate explicit insecure-TLS configuration.

### F09 — MCP parsing and subprocess output could exhaust memory

**Severity: Medium. Confidence: High. Classification: confirmed resource-control
defect; reviewed paths bounded. CWE-400 / CWE-770.**

**Affected files/functions:** `mcphttp/client.go` SSE endpoint/event/JSON parsing and
handshake; `server/customtool.go::execPython` and command output handling.

**Description and historical evidence:** legacy SSE used unbounded newline reads
and discovery could outlive caller cancellation. Python `CombinedOutput` buffered
all stdout/stderr before truncation. A time limit alone does not bound allocation.

**Exploitation scenario / prerequisites:** a configured hostile MCP streams an
oversized/no-newline event or never announces an endpoint; a permitted script emits
output faster than timeout cleanup. Script authorship is already powerful, whereas
remote stream bytes are directly untrusted.

**Potential impact:** memory exhaustion, stuck discovery connections/goroutines and
service unavailability.

**Fix and patch guidance:** current MCP readers bound lines/events/JSON (8 MiB),
handshake time/caller cancellation and redirect/header behavior. Command/Python
output uses a 1 MiB bounded writer that cancels execution on overflow, plus time and
wait bounds. Keep streaming limits before allocation; reject rather than buffer
then truncate. Worker memory/process limits and per-run container retirement
independently constrain native-code consequences.

**Validation:** MCP security tests exercise oversized streams and canceled handshake;
custom-tool tests exercise environment stripping, output cancellation and size caps.
Real worker lifecycle tests confirm detached work stops on close/watchdog. Broader
traffic retention, upload/disk quotas and production load profiles remain separate
work.

### F10 — Mutable build inputs and upstream update identity widened trust

**Severity: Medium. Confidence: High. Classification: confirmed supply-chain/design
gap; unsafe production updater closed, release controls implemented with final
hosted validation pending. CWE-829 / CWE-494.**

**Affected files/functions:** Docker/install/build inputs, release workflows,
`docker-compose.yml`; [selfupdate/github.go](../selfupdate/github.go), `Repo`;
`selfupdate/stage.go`; `server/update.go` apply/rollback gates.

**Description and historical evidence:** the original build used mutable base/tool
versions, remote setup pipelines and tag-based workflow actions. In-app updates
trusted the upstream repository despite distributing a fork; release ZIP/checksums
shared the same publisher, without independent signed provenance. A checksum
protects integrity against corruption, not a malicious trusted publisher.

**Exploitation scenario / prerequisites:** an authorized build/update consumes
compromised or unintended packages/actions/releases. No actual compromise was found.
That historical in-process route is now unavailable in production, including with
the legacy opt-in. Compromise of an actually trusted maintainer/build service
remains a supply-chain assumption, not an exploit demonstrated here.

**Potential impact:** attacker-selected build/runtime code and loss of fork
hardening; impact follows the authority of the build/release/runtime service.

**Fix and patch guidance:** local source builds use lockfiles, image digests and
pinned actions. Production apply/rollback HTTP operations always refuse, startup
rejects `ARTEX_ENABLE_SELF_UPDATE=1`, and production bootstrap/staging never runs
old unsigned files. The repository identity is this personal fork; version checks
are read-only. Legacy staging implementation is reachable only in true Go tests.

[Signed releases](SIGNED-RELEASES.md) fix repository/workflow/issuer/ref/commit/run
identity and require verification before external installation. Publication needs
protected semantic tags, explicit opt-in and exact-image gates; a separate main-only
dry run rehearses real OIDC without publishing a production release. The
[image gate](IMAGE-SECURITY.md) records inventories, complete vulnerabilities/SBOMs,
DB freshness and exact index/config/layer identities. It does not ignore unfixed
issues or authorize package-wide waivers. These controls do not prove an
uncompromised publisher, every hosted repository setting or all future CVE coverage.

**Validation:** local signing/publication regressions, real production refusal
probes and image-gate negatives cover tampered metadata/artifacts, wrong identity,
unsigned staging, incomplete gates and malformed/unsupported image evidence.
There are 19 local image-gate tests, 14 signing tests and 15 publication tests. Fresh
local integration and all four ARM64 gates passed. Real hosted OIDC/amd64 and
protected-tag publication remain separately unverified in
[validation](VALIDATION.md); passing local tests is not a claimed Fulcio or registry
publication result. The former conditional unsigned updater risk is closed, while
release operation still requires the documented trust and verification steps.

### F11 — Model-review failures defaulted to allowing execution

**Severity: Medium. Confidence: High. Classification: confirmed fail-open default;
fixed default, explicit policy review still required. CWE-636; OWASP insecure design.**

**Affected files/functions:** [intercept/intercept.go](../intercept/intercept.go),
`defaultJudgeFailAction`; reviewer parsing/prompt; frontend review defaults;
`agent/capture_approval_test.go`.

**Description and historical evidence:** the default reviewer failure action was
`allow`; errors/invalid decisions selected that action. An integration fixture
explicitly expected an invalid verdict to execute its probe.

**Exploitation scenario / prerequisites:** the optional reviewer is enabled and a
tool reaches it; timeout, provider outage or malformed output occurs. Deliberate
attacker-induced provider failure was not demonstrated. No unauthenticated RCE is
claimed.

**Potential impact:** execution without the meaningful review the operator expected.
Review is an additional approval mechanism, not the worker isolation boundary.

**Fix and patch guidance:** default to `deny`, retain strict verdict parsing and
explicit audit outcomes. In pseudocode: `if reviewError || invalidVerdict { deny }
else { applyValidatedDecision }`. Existing explicit `allow` settings are not silently
rewritten; operators must inspect persisted review policy. English explanation
formatting and legacy compatibility parsing do not relax verdict requirements.

**Validation:** `TestJudgeFailureDefaultIsClosed`, prompt/verdict parsing tests and
approval-lifecycle integration reject invalid/incomplete/ambiguous decisions and
assert denied audit records with no probe execution. Preserve valid decision paths.

### F12 — Asset policy read errors were treated as missing policy

**Severity: Medium. Confidence: High. Classification: confirmed fail-open error
handling; fixed. CWE-636 / CWE-754.**

**Affected files/functions:** [agent/tools_insert.go](../agent/tools_insert.go),
`ToolSet.insertAssets`, global/task asset-rule reads.

**Description and historical evidence:** global rule-load errors were discarded;
task-rule failure skipped that part of policy assembly. Asset insertion continued
without the failed constraints even though the asset-check path already returned
errors.

**Exploitation scenario / prerequisites:** a table-specific permission/query fault
or transient DB failure prevents policy reads while asset writes still work. An
agent registers an otherwise prohibited asset. Causing the DB fault remotely was
not demonstrated and is not assumed.

**Potential impact:** asset-registration policy bypass and potential configured
side effects. This is not independent RCE or proof that coverage labels ever
constrained arbitrary network tools.

**Fix and patch guidance:** read both required policy sets successfully before
writes/callbacks; return a tool error on either failure. A successful empty policy
is distinct from a failed lookup. Current code returns refusal at both reads.
The same fail-closed principle now applies to tool-policy assembly; broker network
CIDRs remain independently controlled rather than inferred from mutable coverage.

**Validation:** [agent/asset_policy_security_test.go](../agent/asset_policy_security_test.go)
faults each policy table only in the explicit isolated DB harness, restores it and
asserts refusal/zero inserted assets. `server/tool_policy_security_test.go` checks
assembly refuses a tool-policy read failure. Preserve tests that empty valid policy
still follows its documented behavior.

### F15 — Artifact paths amplified bounded messages into unbounded directory work

**Severity: Medium. Confidence: High. Classification: confirmed new-boundary
resource-control defect; repaired in the final hardening pass. CWE-400 / CWE-770.**

**Affected files/functions:** [isolation/files.go](../isolation/files.go),
`WriteFiles`, `ApplySnapshot`, `ReadFiles`; current `validateArtifactPath` and
`validateArtifactFiles` guards.

**Description and historical evidence:** the first worker bridge bounded content
bytes and file count but did not bound path length/depth or the number of parent
directories created by `MkdirAll`. A response with 1,000 zero-byte files could
remain within the byte limit while specifying distinct paths with roughly 2,000
components each. The theoretical amplification approached two million directory
creations. This was a defect in an intermediate new implementation, not a claim
that the original archive contained `isolation/`.

**Exploitation scenario / prerequisites:** compromised code in an authorized worker
returns malicious artifact names through the worker response. The API applies
those paths to its task storage; worker memory/tmpfs limits alone do not bound API
inode and directory-creation work. A full two-million-directory exhaustion run was
not performed or required to establish the missing controls.

**Potential impact:** API storage/inode consumption and CPU/I/O denial of service,
possibly affecting other tasks. Traversal was already independently restricted;
this is resource amplification, not an outside-root filesystem escape.

**Fix and patch guidance:** validate the entire returned map before any directory,
file or deletion mutation. Current limits are 1,024 path bytes, 32 components,
255 bytes per component, 1,000 files and 2,048 distinct parent directories, in
addition to content budgets. Reject invalid/conflicting parent paths. `ReadFiles`
also bounds traversed directory depth/count, including empty directories.
The ordering is `validateAll(returned); readCurrent(); applyChanges()`, not
validation interleaved with writes. These are per-operation limits; cumulative
storage quotas remain an operational requirement.

**Validation:** [isolation/files_budget_test.go](../isolation/files_budget_test.go)
covers deep/long/wide paths, rejection before any mutation, preservation of original
files on an invalid snapshot, bounded empty-directory traversal and a valid
boundary-depth example. These regressions, the worker boundary suite and
source/image checks were rerun after this patch; the
[validation record](VALIDATION.md) identifies the publication checkpoint. Keep
them in subsequent release verification.

## 6. Exploitation Chains or Combined-Risk Scenarios

| Historical chain | Necessary prerequisites | Controls now interrupting the chain |
|---|---|---|
| Compromised provider to API command execution (F14) | Administrator tests hostile provider; structured tool-use response | Empty/no-background probe options; real SDK regression; production session late-injection controls |
| MCP origin handoff to credential/internal POST (F01 + F09) | Configured malicious service; endpoint-controlled destination | Same-origin/no-redirect clients, bounded parsing, worker destination firewall |
| Target text to quoted template to API secrets (F02 + F04) | Vulnerable configured template, attacker-influenced parameter, accepted action | Literal argv plus separated credential-free execution; no host fallback |
| Tool-created symlink to outside file API (F03 + original F04) | Symlink-capable actor and authenticated file request | Root-relative opens, regular-file/size limits, no worker host mounts |
| Sibling container to recording relay (F05) | Network adjacency to original all-interface listener | Loopback-only listener, no proxy publication and no worker API-proxy route |
| Stolen session to intended operator capabilities (F07 + original F04) | Actual token exposure/XSS/log or transport compromise | Cookie/query changes, revocation, origin checks and worker separation; operator token remains powerful |
| Reviewer/asset read failure to bypassed policy (F11/F12) | Review/DB fault with relevant action still possible | Explicit refusal, negative-side-effect tests; independent broker policy |
| Worker response to API directory/inode exhaustion (F15) | Authorized compromised worker; many empty deep artifact paths | Entire-map validation before mutations, path/depth/parent-directory budgets and bounded traversal |
| Foreground cancellation to continuing detached scans | Native code starts setsid/double-fork descendants | Urgent run-container removal, separate lease channel, UID-protected watchdog and replay ledger |
| Compromised release channel to runtime execution (F10) | Compromise of the deliberately trusted build/publisher or unverified external installation | Production updater permanently refused; exact-identity signatures and image gates constrain intended channels; hosted/trusted-publisher assumptions remain |

No combined chain here asserts that an unproven prerequisite occurred. In
particular, target prompt injection, stored XSS, cloud credential theft and kernel
escape were not fabricated to turn conditional findings into demonstrated exploits.

## 7. Dependency and Configuration Risks

### Advisory results and supply-chain scope

The latest full frontend npm audit reports **zero**, as does its production-only
audit. Earlier npm output listed six High graph entries inherited from one
Picomatch advisory,
[GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf).
The official GitHub advisory API recorded withdrawal at
`2026-11-18T15:38:56Z`; the registry later stopped returning those entries. Six graph
entries were not six independent vulnerabilities.

[scripts/audit-npm.py](../scripts/audit-npm.py) preserves raw npm results and checks
every advisory leaf against the official advisory API. An active, unknown, missing,
cyclic or unverifiable leaf fails the check. No package-wide ignore, version spoof,
forced major downgrade or claimed fix without evidence was used. A later npm run
can legitimately differ; re-run at release time. Helper/browser lockfiles were
also checked separately and report zero.

Official `govulncheck` v1.10.0 reported zero source-symbol findings for the checked
Go graph, using its recorded vulnerability database. This does not audit all
external SDK behavior, test-only code, alternate build tags, OS packages or future
advisories. F14 illustrates why advisory scanning cannot replace behavioral review
of an otherwise unflagged dependency.

Gitleaks reviewed matches were synthetic masking/authentication fixtures; no
confirmed live credential leak was found. This is limited point-in-time evidence,
not proof of absence in external account history, backups or future Git commits.
The publication preparation excludes credentials, private state, raw captures,
installed dependencies, binaries, caches and Git history, and records a manifest.


### Runtime images, signatures and evidence storage

The follow-up adds [image inventory/CVE gates](IMAGE-SECURITY.md),
[signed release verification](SIGNED-RELEASES.md) and
[evidence storage controls](EVIDENCE-STORAGE.md). The worker uses Ubuntu 26.04 and
version-pinned Node/npm/browser tooling; the API has no assessment toolchain.
Scanner completeness requires the relevant Go and npm/browser inventories, not
only an OS-only base scan. Exact layer/config/index bindings prevent a changed tag
or Docker/Trivy identifier mismatch from being accepted as the intended image.

A current worker report still contains one raw High Picomatch alert. The official
API confirms the corresponding advisory is withdrawn; this is tracked through a
live exact CVE/GHSA/npm-identity proof rather than a static risk exception. Raw
counts remain in reports. All four final ARM64 gates passed with zero blocking findings; the worker retains
raw High 1 / Medium 132 / Low 14 and the broker retains Medium 1. Exact identities and the
pending hosted amd64/signing rehearsal are recorded in [validation](VALIDATION.md).
Neither existing zero npm/Go findings nor these gates certify all OS/runtime code.

### Deployment assumptions that must remain explicit

- Keep the API loopback-published or behind a reviewed authenticated-TLS ingress;
  keep broker and PostgreSQL private. Do not publish broker/proxy ports or mount
  Docker into API/workers.
- Use a dedicated Docker VM/daemon. The trusted broker's socket access is host-level
  authority; Docker/kernel/operator compromise is outside the worker guarantee.
- Review management denies and approved assessment CIDRs. All Docker networks are
  inspected when workers are created; establish management networks first and
  restart the broker after topology/policy changes. DNS resolvers are explicit
  network capabilities; use an assessment resolver where required.
- PostgreSQL bridge transport uses `sslmode=disable` in the local topology. Use
  authenticated TLS and least-privilege database roles across remote/shared networks.
  No public PostgreSQL exposure is inferred from that local DSN alone.
- Preserve private signing-key/state volumes and test backup/rotation. Do not copy
  generated secrets, replay ledgers, databases or evidence into the Git repository.
- Keep development servers private. Static production export and Next development
  server have different exposure; do not infer identical reachability from npm
  package presence alone.
- Image scans and signed verification now have checked-in controls. Exact final
  image results, real OIDC/publication, protected branches/tags/environments and
  registry settings still require release-environment evidence; source tests do
  not establish that hosted settings are configured.

## 8. Secure Design Gaps

These are remaining limitations/recommendations, not invented additional confirmed
exploits.

**Identity and tenancy:** authentication represents one trusted operator. Companies,
tasks and agent visibility organize resources; they do not establish tenant
principals or per-user ownership. Do not market this as secure shared hosting.
Multi-tenant support needs explicit identities/roles, object authorization on every
query/file/stream, secret ownership, quotas and independent storage/worker policy.

**Trusted control-plane integrations:** typed model/search providers, notification
services and administrator-configured endpoints retain control-plane authority.
A model response cannot gain executable probe tools after F14, but a compromised
API or deliberately misconfigured provider remains outside the worker boundary.
Review destination credentials and provider retention contracts before sending real
target data. Explicit MCP credentials grant capabilities to their run and must be
scoped accordingly.

**Audit and attribution:** in-process bounded logs and database records are useful
but not independently immutable. Export attempted/approved/denied/executed outcomes,
operator/session/run/task identity, policy version, input digest and result to a
restricted append-only sink. Shared identities limit attribution. Avoid putting
bearer values, target credentials or raw private evidence into ordinary logs.

**Evidence privacy and retention:** raw requests/responses, uploads and findings
can contain personal data and target secrets. New whole-bucket legal holds and
30-day raw retention are documented in [evidence storage](EVIDENCE-STORAGE.md).
Raw SDK debug transcripts default off for ordinary binaries; Compose enables
bounded ephemeral tmpfs. Live SDK debug files are excluded from workspace legal
holds and are destroyed on restart, so they are not durable held evidence. PostgreSQL UI conversation/activity records are
separate, so disabling raw dumps is not deletion of that history. Fresh integration
checks cover bounded model-context replay and API output handling as well as storage.
Backup encryption, provider retention, legal requirements and verified deletion
still need per-assessment operational decisions. Holds are not forensic immutability.

**Resource budgets:** evidence, traffic and workspace now have independent byte
and entry admission quotas, in addition to body/exchange limits, message/output
bounds and worker cgroups. These are not a total VM, PostgreSQL/WAL, log, backup
or Docker-layer capacity cap. Distributed abuse, connection limits and those other
stores remain separate. Do not raise a message limit to bypass storage admission;
the bridge still synchronizes at most 1,000 regular files/4 MiB, with larger
unsynchronized artifacts and interactive state remaining ephemeral.

**Evidence consistency and operational failure:** snapshots read actual tmpfs and
then pause/retire matching runs. They are best-effort copies of worker-controlled
data, not atomic forensic acquisitions. Sibling path conflicts/oversize data fail
preservation explicitly. A pause longer than the lease can cause expiry on resume.
Control-plane loss does not guarantee immediate cancellation; the watchdog supplies
the independent lease bound. Preserve needed evidence before session close.

**Feature limits:** privileged raw-packet scans are unsupported; use unprivileged
TCP-connect scans. Workers cannot tunnel through the API recorder, and a distributed
capture/import pipeline is absent. Prompt translation was checked for English and
regressions, not proven behaviorally equivalent by live-model benchmarks. The 480
exact reviewed technical-data exceptions preserve historical identifiers, provider
signatures and international evidence/tests without exempting whole files.

## 9. Recommended Remediation Roadmap

| Priority | Action | Acceptance evidence |
|---|---|---|
| Before each deployment | Review VM/daemon trust, CIDRs/management denies, TLS, secrets/state volumes, public ports, explicit review-failure policy | Deployment inspection plus allowed/denied synthetic tests; no local fallback or real secret in workers |
| Before each publication | Re-run clean-source validation/advisory/secret/English checks, review source manifest and exclusions, preserve license attribution | Reproducible checks and manifest; no credentials/state/raw captures/history in upload |
| Before release activation | Complete real main-only OIDC rehearsal, verify protected-tag policy and repeat final image/signature checks | Exact repository/workflow/tag/commit/run evidence; tampered/wrong/unsigned artifacts refused; no production in-process updater |
| Each later source/runtime change | Repeat the final Go/PostgreSQL/Docker/image regressions as needed; the current local follow-up passed | Current results: 869 PG cases + 1 optional skip, 13 additional suites, 52 worker, 12 API, 11 Compose; exact IDs and limits in validation |
| Next operations iteration | Exercise backups/rotation/deletion, enforce database/log/VM capacity and external append-only audit | Restore drills, explicit capacity boundaries and attributable audit evidence |
| Before broader access | Independent repeat audit and realistic controlled load/hostile-content review | Documented negative tests and deployment-specific findings |
| Before untrusted multiuser hosting | Design identities, roles, ownership, tenant secrets/storage/execution and per-tenant budgets | Separate threat model and end-to-end authorization/isolation tests; current single-operator assumptions removed explicitly |

Keep regression coverage for all repaired paths. Operator policy changes can
intentionally authorize dangerous actions; do not silently reclassify those grants
as model-side scope enforcement or assume a prompt judge can repair them.

## 10. Security Test Plan

### Earlier completed checkpoint

The following integration counts predate the latest image/release/storage follow-up.
They include F15 artifact-budget regressions and then-rebuilt images. Refer to
[VALIDATION.md](VALIDATION.md) for the fresh final-source section; an earlier image
must never be assumed to cover a subsequent patch:

- 817 passing PostgreSQL-backed cases including subtests, with one optional
  external-service test skipped; package/build/vet checks also passed.
- 52 live worker isolation checks using only disposable synthetic targets. Coverage
  includes policy/auth admission, actual daemon/RPC-client privileges, absent
  secrets/socket mounts, allowed/adjacent/gateway destinations, firewall/watchdog
  restrictions, sibling/task files, urgent cancellation, replay, detached scans,
  90-second orphan expiry with a renewed sibling, real tmpfs snapshots, conflict/
  oversize failure, all-run retirement, crash recovery, ledger persistence and cleanup.
- 12 live API/security checks including cookie/session/origin/body/update/execution
  gates and persisted logout revocation.
- At that checkpoint, 23 frontend tests and a static export/generated-HTML check
  passed. The current UI adds three release-capability/clipboard tests: 26 tests,
  clean-source TypeScript and 3,086 English keys now pass. Biome has zero errors,
  124 warnings and 659 infos. A fresh final image build/export is still required.
- At that earlier checkpoint, zero full/production frontend npm findings and zero
  reported Go source vulnerabilities, plus helper audits, reviewed secret results,
  translation/source publication checks and documentation-link validation.
- Targeted race-enabled isolation/agent/worker tests, malicious provider probe
  regression, synthetic HTTP recon-policy tests and disposable Chromium helper tests.

[VALIDATION.md](VALIDATION.md) is the authoritative reproducibility companion;
passing counts describe their stated checkpoints and are not an external certification.
The current 19 image-gate, 14 signing and 15 publication tests are local controls.
Fresh local Go/storage/Docker records now include 869 PG cases, 13 additional
suites, full focused races/vet, a Windows amd64 cross-build, four source builds,
52 worker / 12 API / 11 Compose passes
and all four ARM64 image gates with 0 blocking. No passing remote amd64/OIDC
Actions run or production publication is inferred from these local results.

### Required release and regression cases

| Area | Negative and positive checks |
|---|---|
| Auth/session | Wrong/missing setup secret, concurrent first init, DB errors, expiry/algorithm/subject tampering, password revocation, durable logout, query-token refusal, same-origin mutation and valid SSE reconnect |
| Authorization | Trusted task/run attribution; policy read failure produces no writes/tools; future endpoints must not introduce an unreviewed user/tenant assumption |
| Injection/files | Quoted/metacharacter/recursive argv values stay literal; utility option boundaries; symlink root/parent, traversal, path/depth/parent-directory amplification, concurrent replacement, special files, upload/download/skill CRUD and bounded archive restore |
| MCP/provider | Same-origin relative endpoint works; foreign/downgrade/port/redirect fails before credentials; oversized/stalled streams cancel; malicious connection-test tool response creates no canary |
| Worker boundary | Tool/daemon/client caps/UID/NNP, no host mounts/secrets/socket, DNS/redirect/resolved IP behavior, management/metadata/Docker/IPv6 denial, allowed private targets, no local fallback |
| Lifecycle | Close under long foreground RPC, replay rejection before/after restart, sibling survival, detached/double-fork termination, stopped leases, expiry and task-wide snapshot/retirement/recovery |
| Browser/helper | Hostile evidence/Markdown/URL rendering; mock default, same-origin safe forwarding, cookie collector/redirect refusal, bounded responses and explicit proxy behavior |
| Resources/deployment | Slow headers/bodies, bcrypt admission, process/output/message bounds, storage quotas, container budgets, port/mount inspection, key persistence/rotation, backup/restore/deletion and release provenance |

Run Docker host-alias fixtures only on a dedicated disposable daemon; they grant
NET_ADMIN/host networking solely to the synthetic fixture and remove allocated
aliases and state afterward. Do not probe live targets or metadata credentials
without separate authorization. An optional live-provider skip is a recorded
limit, not a passing external-service integration test.

## 11. Open Questions and Assumptions

1. Will deployment remain single-operator on a dedicated VM/daemon? Shared
   untrusted users, operator accounts or daemons invalidate the current assurance
   boundary and require redesign.
2. Are every real management/host/cloud-sensitive range and external integration
   destination represented in operator policy? Source cannot determine deployment
   topology, future network additions or account permissions.
3. Which integration/model/search/notification providers receive actual evidence,
   and what retention, region, access and contractual controls govern it? These
   require operator/provider evidence.
4. Are target credentials synthetic where required, integration accounts narrowly
   scoped and insecure TLS exceptions deliberately reviewed? Compatibility options
   must not quietly become blanket production trust.
5. Have the real main-only OIDC rehearsal and protected-tag release verification
   completed for the exact intended commit/run? Production in-process updates
   cannot be enabled; signed external installation remains mandatory.
6. Are repository/tag/environment protections, registry policies, final ID-bound
   scans, backups, key rotation, legal holds, retention and separate database/log
   capacity configured and exercised in the actual environment? Checked-in controls
   and local tests do not prove those deployment settings.
7. Is forensic-atomic evidence preservation, large streamed transfer, privileged
   raw scanning or distributed recording required? Those capabilities are outside
   this version's supported behavior.
8. Has an independent reviewer repeated the final assessment, including external
   SDK upgrade behavior and realistically hostile inputs under deployment limits?
   Current coverage is substantial but does not exhaust every code path or future
   dependency/runtime combination.

No private host paths, credentials or raw captures are included in this report.
Historical findings are retained to explain risk and regression obligations, not
to misrepresent the current hardened implementation as the original archive.

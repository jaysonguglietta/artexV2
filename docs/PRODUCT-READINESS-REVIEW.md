# Product and application readiness review

Reviewed 18 November 2026 against `e30fe09` (`artexV2-upload`). This supplies
sections 12–19 after the [security audit's sections 1–11](SECURITY-AUDIT.md).
It is a review and proposed backlog, not authorization to implement the proposals.
The separately requested image, tag-protection, database/log capacity and backup
work was in progress at the reviewed baseline and is identified below rather than
counted as missing work.

**19 November follow-up:** those four authorized workstreams now have implemented
controls and scoped local proof: active hosted tag rules; physical database/log
bounds; authenticated encrypted backups with a 22-check disposable restore; and
actual package-manager remediation plus stricter image/publication policy. See
[validation](VALIDATION.md), [capacity/backups](POSTGRES-BACKUP.md),
[tag protection](RELEASE-TAG-PROTECTION.md) and [image security](IMAGE-SECURITY.md).
The actual final ARM64 reviewed-findings scan passed with 94 exact reviews and
zero blocking: raw worker inventory retains 85 Medium/9 Low active records, while
the other images report zero. These explicit contained, expiring reviews accept
residual risk; they are not fixes or a zero-active result. High/Critical/Unknown
and fixable findings remain blocking. The final rebuilt implementation passed
**53 live worker checks**, **six actual worker/browser checks**, **22 encrypted
backup/restore checks** and a fresh isolation race run in 8.176 seconds, including
the concurrent-removal and adjacent cleanup-failure fix. Host activation/rotation,
17 Compose checks and 26 image-policy tests have their own recorded passes.
The current 188 reviews cover 94 exact native records per platform and expire
`2026-12-19T12:00:00Z`; amd64 entries followed actual hosted inventory verification.

Current `375b2c3…` subsequently passed all six hosted Go jobs, all eight native
reviewed-image gates and documentation, was fast-forwarded to main, then passed
the real main OIDC rehearsal. [Validation](VALIDATION.md) and sanitized runtime/image
evidence bind those source/run/report identities. Earlier startup and compressible
quota-test failures are preserved as historical checkpoints; the corrected inline
payload fixture passed. The prepared source archive is credential-scanned and
compared with Git; its companion receipt records the exact publication identity. No version tag, formal release, package or enabling variable was created.
The feature inventory, in-progress labels and
PR-01–PR-09 entries below remain the `e30fe09` product-review baseline; those nine
backlog proposals were not implemented by this security follow-up. For operational
portions of PR-06/07/08, consult the new evidence rather than assuming the original
gap still exists.
No complete browser-to-assessment workflow is newly certified.

## 12. Product Review Executive Summary

ARTEX V2 helps a **single trusted technical operator** define an authorized
assessment, coordinate model-driven exploration, review execution, preserve
findings/evidence and perform targeted retests. Secondary consumers are people
receiving an exported assessment and the operator maintaining its dedicated VM.
They are not separate application roles. The operating model is self-hosted AGPL
software with operator-supplied models/integrations, not a hosted subscription or
multi-tenant service. Billing, invitations, public signup and team collaboration
would add a different product and trust model; none is recommended here.

The product has substantial real implementation: PostgreSQL task/asset/finding
records, admission and cancellation, isolated execution, review records, exports,
archive recovery, retesting, notifications and scoped storage. Its backend and
operational boundaries have meaningful synthetic/integration coverage. This is
more mature than a UI mock, but **component and API passes are not proof of the
complete browser-to-deliverable journey or useful autonomous assessment quality**.
The repository also intentionally supports a separate synthetic preview mode.

The most consequential product gaps found are:

- First-scan readiness is split between the task form, model settings and manual
  broker policy files; the interface does not explain the whole dependency chain.
- Retesting has a real lifecycle, but its history is omitted from the portable
  report/archive workflow. A recipient can miss the evidence behind a fixed state.
- Some failures look like empty/stale data, and a committed-but-paused task is
  represented to its creator as a generic creation error.
- Tool/model detail rows expose click-only entry points despite an existing
  keyboard-capable pattern in Findings.
- Storage holds and recovery APIs are operationally useful but lack an equivalent
  in-app status/recovery surface. Host-wide capacity and verified backups are
  being addressed by the current, separately authorized work.

The proposed backlog has no demonstrated product P0. Core API/worker journeys
have relevant successful evidence, and missing live-user validation is not itself
proof of a release-blocking defect. Dependable use still warrants the P1 work
below. Security severity and feature priority are separate.

### Scope and evidence limits

Reviewed the navigation/routes, task creation and detail views, findings/retests,
reports/exports, command/model-record tables, settings, workspace, integration
handlers, archive model, storage APIs, schema, relevant tests and current operator
and validation documents. Citations below identify the precise slice supporting
a status; route presence alone never establishes success.

The [validation record](VALIDATION.md) documents tested application source
`d06f7d8` and its documentation-only descendant `e30fe09`: local PostgreSQL/worker/
Compose checks and hosted Go/frontend/image/keyless runs. This review relies on
that recorded evidence, code inspection and three disposable frontend probes.
The probes used an external temporary source copy, matching locked dependencies,
jsdom and mocked fetch/API responses; no source dependency symlink or new
production test was left behind. They confirmed: Commands displays empty after a
read failure; its detail row opens on click but not Enter; and `ApiError` retains
HTTP 507 while dropping the committed-task recovery fields. These are three
review observations, not additions to the maintained frontend test count.
It did **not** run browser end-to-end,
keyboard/screen-reader/mobile sessions, a real model assessment, ScopeSentry,
SMTP/chat-provider delivery, or destructive restore against a live installation.
New code arriving after `e30fe09` must receive its owner's own verification; it
is not silently covered by those earlier runs. No provider requests were made.

## 13. Existing Feature Inventory

Statuses use the rubric literally. **Verified working** below is qualified to the
exercised API/unit/operational slice; the corresponding browser journey remains
unverified unless stated. Real deployment data persists in PostgreSQL plus bounded
files/traffic SQLite; mock mode is a separate compile-time switch.

| Feature, intended user and completion outcome | Entry points, data and persistence | Implementation status and evidence | Main limitation/dependency |
|---|---|---|---|
| Initial setup/login/session lifecycle: operator claims and accesses the instance | `/setup`, `/login`; `server/auth.go`; settings and auth revocations in PostgreSQL | **Verified working — API/Compose slice.** [Auth integration](../server/auth_init_db_test.go) `TestAuthInitRequiresSetupTokenDB`, [session tests](../server/session_security_test.go), recorded Compose acceptance | No browser setup walkthrough exercised here; single principal, not per-user roles. Password recovery is an operator procedure. |
| Dashboard and service diagnostics: operator sees activity, counts and server logs | `/dashboard`, `/system/logs`; dashboard polls real API sources; model usage/health/log records in PostgreSQL | **Implemented but unverified — composed browser experience.** [Task metric tests](../db/task_list_performance_test.go) support specific aggregates; no whole dashboard or log-browser acceptance run is claimed | Polling retries silently on transient failures; freshness/large-history rendering and external immutable audit are not established. |
| Tasks, categories, templates, inherited context and model chains: operator defines repeatable work | `/function/tasks`; [task form](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/tasks/page.tsx), `POST /api/tasks`; tasks/categories/templates/relations in [schema](../db/schema.sql) | **Verified working — API/DB slice.** [Template CRUD](../server/task_templates_test.go), [task metadata](../server/task_metadata_test.go), [task data tests](../db/tasks_test.go) | Browser creation and repeated-edit flows untested; task form lacks consolidated worker/network readiness (PR-01). |
| Draft attachments: operator supplies files that reach the new task | Task form; [upload API](../server/chatupload.go), `draft_id`; task workspace files | **Verified working — transfer/API slice.** [Staging regressions](../server/staged_uploads_test.go) cover bounds, copy-before-launch and created-paused failure | The frontend does not preserve structured recovery metadata from a partial commit (PR-03). Transfer budget remains deliberate. |
| Queue, pause/resume, concurrency and timeout: operator controls active assessments | Task list/detail; [control service](../server/task_control.go), [admission tests](../server/task_admission_test.go); persisted task lifecycle | **Verified working — synthetic/API slice.** FIFO, paused/queued transitions, profile availability and control routes have tests | User understanding of combined task/provider/worker states and multi-tab behavior has not been exercised. |
| Executable agent tools: operator runs only approved target operations | API → broker → per-run worker; [isolation](../isolation/broker.go), private policy and bounded artifacts | **Verified working — synthetic worker lifecycle.** Recorded 52 live checks, plus [isolation tests](../isolation/security_test.go) | No live-provider autonomous assessment quality claim. Policy grants remain out-of-band; no privileged raw-packet scanning or worker-to-API recorder pipeline. |
| Planning, goals and exploration/asset graphs: operator follows evidence-backed progress | Task detail graph/coverage/goals tabs; exploration nodes/edges/anchors, companies/assets/task scope | **Implemented but unverified — full planning/graph journey.** Asset/scope parsing and activity pagination have [DB tests](../db/company_scope_test.go) and [paging tests](../db/activity_page_test.go); [goal response tests](../server/goals_api_localized_test.go) cover validation errors, not a full successful assessment | Full interactive graph usability, large graph rendering and assessment completeness are **implemented but unverified** here. Coverage is not network authorization. |
| Chat, mentions and side questions: operator intervenes without losing task context | `/chat`, task detail; conversations/activity and side-question tables | **Verified working — relevant tests.** [Mention context](../server/chat_mentions_test.go), [side-question boundaries](../sidequestion/context_test.go) | Browser streaming/reconnect/scroll and real model responses are not validated here; no separate collaborator identities. |
| Model profiles, retries and quota failover: operator keeps authorized work running | `/system/llm`; profiles/health/task chain; [task resolver](../server/task_llm.go) | **Verified working — synthetic provider/DB tests.** [Task model tests](../server/task_llm_test.go), [pool tests](../llmpool/pool_test.go), safe [connection probe](../agent/connection_security_test.go) | Actual external provider credentials, billing behavior and every provider format remain unverified. |
| Findings, disposition, detailed report and deepening: operator triages a result | `/function/findings` and detail; findings/evidence versions; finding workflow tools | **Verified working — API/workflow slices.** [Finding workflow tests](../server/finding_workflow_test.go), [traffic/export tests](../server/finding_traffic_test.go) | Useful report quality and complete browser editing were not exercised. Evidence/version staleness must remain visible. |
| Manual retest: operator records still-reproducible/fixed/inconclusive outcomes | Finding panel/task retest tab; [retest handler](../server/finding_retests.go), [immutable retest records](../db/finding_retests.go) | **Verified working — synthetic lifecycle.** [Retest tests](../server/finding_retests_test.go) cover deduplication, stops/failures and fixed disposition | **Partial portable workflow:** retest history/conversations are not exported/archived with the finding (PR-02). |
| Finding export and task report: operator hands off selected/filtered results | Findings export dialog; `GET /api/exploration/findings/export`; Markdown/ZIP/CSV/JSON; [report renderer](../report/report.go) | **Verified working — export/API slice.** `TestFindingTrafficAPIAndExport`; task report error/retry states exist in [ReportTab](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/tasks/detail/_tabs/report-tab.tsx) | No recipient usability review; report input lacks retest results. Task report copy is not the same deliverable as filtered finding export. |
| Traffic/evidence binding: operator preserves a finding's HTTP proof | `/function/traffic`, finding evidence panel; traffic SQLite, snapshots/bindings and evidence bodies | **Verified working — synthetic/API slice.** [Traffic evidence round-trip](../server/finding_traffic_test.go), [hold tests](../server/finding_traffic_hold_test.go) | Workers cannot transparently populate the API recorder. Raw recorder TTL and retained finding-bound copies have different lifetimes. |
| Archive/restore/delete: operator moves completed work off active views and recovers it | Task archive panel; asynchronous archive state, tables and `.tar.zst` packages | **Verified working — relevant round-trip/failure tests.** [Package journals](../server/task_archive_package_test.go), [archive data tests](../db/task_archives_test.go) | Retest history omitted; raw captures intentionally omitted from new archives; worker snapshots are not forensic-atomic. |
| Tool activity and model recordings: operator inspects execution provenance | `/function/commands`, `/function/llm-records`; paginated activity/record APIs and persisted records | **Partial — inspected review UX.** A disposable component probe exercised Commands click details and confirmed its empty-on-error/keyboard gaps; [raw-wire persistence](../server/llmrec_raw_test.go) is tested separately. Real browser/service journey remains unverified | Failed reads can look empty/stale; click-only details impede keyboard use (PR-03/05). No whole-history browser exercise. |
| Approval/review history: operator permits/refuses and explains actions | `/system/intercept/approvals`, task/chat records; intercept policies/pending decisions | **Verified working — server/agent slice.** [Review regression](../server/intercept_review_test.go), [approval integration](../agent/capture_approval_test.go) | Full browser focus, timeout and interrupted-approval interaction remains unverified; review is not isolation. |
| Files: operator uploads, edits, downloads and deletes workspace artifacts | `/function/workspace`; [workspace handler](../server/workspace.go), root-anchored bounded files | **Verified working — API security/admission slices.** [File tests](../server/files_security_test.go), [quota tests](../server/storage_test.go) | No browser file workflow run; protected internal roots and holds intentionally refuse some mutations. |
| Skills, tools, agents and MCP: operator configures assessment capabilities | `/system/{skills,tools,agents,mcp}`; versioned prompts/config/tool bindings plus worker-side integrations | **Verified working — specific utility/confinement slices**, not every editor flow. [Custom-tool tests](../server/customtool_test.go), [skill tests](../server/platform_skill_security_test.go), [MCP tests](../mcphttp/security_test.go) | Installed worker capabilities and integration scope are prerequisites. API-side Python setting/detection is misleading for isolated script execution (PR-01). |
| ScopeSentry import: operator imports authorized inventory | `/function/sync`; [status/project/task/ingest handlers](../server/sync_scopesentry.go); companies/assets/scopes | **Implemented but unverified — real integration.** Parsing/English error tests exist; no live service proof found in reviewed evidence | Requires operator endpoint/key and supported remote schema; imported ownership does not grant scan permission. |
| Notifications: operator receives findings/status/digest and retries failures | `/system/notify`; channel/event/delivery tables and dispatcher | **Verified working — local test receivers.** [Notification API tests](../server/notify_api_test.go) cover delivery, retry, digest, filtering and secret masking | Actual SMTP/DingTalk/Feishu/WeCom accounts and human receipt not tested here; public-base URL configuration affects links. |
| Storage quotas, TTL and legal holds: operator protects evidence/capacity | `GET /api/storage`, hold/purge APIs; evidence/traffic/workspace budgets and state files | **Verified working — API/DB/operational slices.** [Storage tests](../server/storage_test.go), [storage guide](EVIDENCE-STORAGE.md) | **Partial operator UX:** no corresponding frontend usage/hold/recovery view found. These are not PG/WAL/host log limits (PR-04/07). |
| Version/read-only release UI: operator checks and copies version, follows verification guidance | Settings update card; fork release query; external verification scripts | **Verified working — component tests.** [UpdateCard tests](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/system/settings/_components/update-card.test.tsx) deny missing/false execution capability | In-process apply/rollback intentionally unavailable; no actual protected-tag production release claimed. |
| Image gates and keyless release rehearsal: maintainer validates supply chain | [Image guide](IMAGE-SECURITY.md), [signed guide](SIGNED-RELEASES.md), workflows/scripts and recorded hosted runs | **Verified working — recorded two-arch scans and real main-only rehearsal.** [Validation evidence](VALIDATION.md) | Current Medium/Low remediation and actual hosted tag protections are **in progress**, not reverified by this review (PR-08). |
| Database/log hard caps and encrypted verified restore: VM operator keeps/rebuilds deployment | Existing bucket quotas/docs plus separately requested new host/backup tooling | **Partial at baseline; follow-up in progress.** New PG/log/backup implementation is owned by current workstream, not assumed absent or passed | Wait for actual-host hard-limit and disposable restore evidence; a named-volume `size` option alone is not proof (PR-06/07). |
| Synthetic preview | `NEXT_PUBLIC_MOCK=1`; [mock switch](../web/src/lib/mock/enabled.ts), in-memory handlers/data and canned report | **Mocked.** Explicit compile-time preview, not production PostgreSQL/provider behavior | Useful for UI exploration; resets/state simulation and canned findings cannot establish assessment success. |
| Shared accounts, teams, subscriptions and customer billing | No requirement or implementation located in reviewed routes/schema | **Not found; not recommended** for this product scope | Adding them would require a new authorization, ownership and operating model. |

No production feature is labeled **Broken** merely because it was not exercised.
Confirmed interface limitations below are narrower than claiming the underlying
backend feature is broken.

## 14. User Journey Review

| Journey | Current path and expected completion | Friction/failure and user consequence | Recommendation |
|---|---|---|---|
| First visit | Source/Compose setup → `/setup` or preset password → `/login` → dashboard | Core API setup is tested; an operator must still assemble model and worker prerequisites across settings/docs. No browser first-run session was observed. | PR-01; acceptance plan |
| First successful authorized scan | Create task/model chain/attachments → task ID → approve exact `task-ID` CIDRs in private broker policy → restart broker → run/resume → inspect findings | Task creation launches asynchronously, but policy is keyed by the newly assigned task ID. Missing rules fail safely; readiness and repair are outside the form. Coverage rules can be mistaken for executable network grants. | PR-01 |
| Repeated assessment | Template/category/source tasks/company/model chain → task detail → pause/resume/inspect | Useful reuse exists. No evidenced ready-state summary distinguishes provider unavailable, missing worker grant and actual queued execution before a new start. | PR-01; PR-09 hypothesis |
| Correct a failed submission | Draft upload → create → attachment copy failure returns `created=true`, `task_id`, paused state | [Client transport](../web/src/lib/api.ts) retains status in `ApiError` but discards structured recovery fields; [form](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/tasks/page.tsx) shows a generic creation-failed toast and retains the form. A retry can create another task despite the original commit. This is a code-backed recovery gap, not a reproduced duplicate. | PR-03 |
| Review execution | Commands/Model recordings filters → row → detail drawer | Commands erases rows on read failure; Model recordings can retain old rows without a failure indicator. Their detail rows have click handlers but no equivalent keyboard trigger. A user can misread missing/stale evidence or be unable to inspect it. | PR-03/05 |
| Verify a fix and hand off | Finding → retest conversation → result/disposition → export/report/archive | Retest records exist but portable task archive tables and report inputs omit them. A handoff loses the basis for the fix claim. | PR-02 |
| Preserve or free space | Storage API/status → review bucket/hold/retention → explicit purge/release → retry | Documented curl/API workflow works; ordinary UI errors do not lead to the relevant storage state or guided recovery. | PR-04; PR-07 in progress |
| Transfer/archive/delete | Pause/terminal task → archive job → package → restore or delete | Journals and bounded restore are tested. Raw captures omitted by design; retests and full-environment recovery are different scopes. Holds may correctly refuse deletion. | PR-02/04/06 |
| Upgrade/rollback/recover VM | Check version → verify expected tag/commit/run/digests externally → stop/backup → install/deploy → validate | Read-only UI and signing rehearsal are verified; actual protected-tag publication and a complete tested restore contract are separate. Current implementation work targets these operational gaps. | PR-06/08 |
| Import or alert externally | Configure ScopeSentry/channel → test/select/filter → import/deliver/history | Local notification behavior has tests, but live account/schema/connectivity and real recipient confirmation remain unverified. | Acceptance plan before depending on an integration |
| Collaboration/handoff | Operator exports a deliverable to an external reviewer | This is artifact handoff, not simultaneous multiuser editing. Shared-account collaboration is not a supported journey. | PR-02; no teams feature proposed |

## 15. Prioritized Feature Backlog

Recommendations are proposals unless explicitly identified as the user's current
implementation work. A **confirmed gap** cites inspected code/document behavior;
a **likely gap** has incomplete evidence; a **product hypothesis** needs research.
Targets below are proposed acceptance/success measures, not measured baselines.

| ID | Recommendation | Category / type | Priority | Effort | Confidence | Key dependency / current state |
|---|---|---|---|---|---|---|
| PR-01 | Read-only assessment/runtime readiness | Core workflow gap / add | P1 | Medium | High | Broker status contract; proposed, not authorized implementation |
| PR-02 | Portable retest outcomes and evidence | Core workflow gap / complete | P1 | Large | High | Versioned archive/export schema; proposed |
| PR-03 | Honest failures and safe committed-task recovery | Existing feature improvement / improve | P1 | Large | High | Typed API errors and task retry identity; proposed |
| PR-04 | Storage/hold recovery surface | Production readiness / add | P2 | Medium | High | Existing storage APIs; coordinate new capacity/backup work |
| PR-05 | Keyboard-complete execution/evidence review | Existing feature improvement / complete | P1 | Medium | High | Representative browser test harness; proposed |
| PR-06 | Verified whole-installation backup/recovery | Production readiness / complete | P1 | Large | High | **Authorized work in progress**, new backup tooling/guide |
| PR-07 | Actual PostgreSQL/log capacity boundaries | Production readiness / complete | P1 | Large | High | **Authorized work in progress**, actual VM/filesystem/log policy |
| PR-08 | Release posture and remaining image findings | Production readiness / complete | P1 | Medium | High | **Authorized work in progress**, hosted rules + exact scan proof |
| PR-09 | Repeat-assessment delta view | Strategic enhancement / add | P3 | Large | Low | Research first; stable matching semantics and PR-02 |

### PR-01 — Read-only assessment/runtime readiness

**Classification:** confirmed gap. **User/job:** the trusted operator needs to
know why an assessment can or cannot run before spending model turns or searching
logs. **Current evidence:** `createTask` in [server/server.go](../server/server.go)
launches asynchronously; [worker setup](WORKER-ISOLATION.md) requires a task-ID
policy grant and broker restart. No readiness route/view was found. Settings also
persists/detects an API Python path, while `remoteCustomTool` sends the script to
`ExecuteIsolatedCustomTool`, whose DB-free worker resolves its own interpreter in
[server/customtool.go](../server/customtool.go).

**Proposed behavior/smallest complete scope:** task detail and creation completion
show read-only checks for model resolution, broker reachability, exact task policy
presence/version, image identity and available worker capabilities. Each refusal
links to the precise operator repair. Retire or relabel ineffective API-side
interpreter controls and preserve any stored value for an explicit migration.

**Permissions/rules/edges/exclusions:** authenticated operator only; no Docker
socket or secret output in the API. Readiness must not modify private policy,
authorize a discovered asset or let a model broaden CIDRs. Handle stale broker
policy, unavailable provider and draft-created task IDs. Do not build a general
in-browser host/package manager or silently auto-grant targets.

**Acceptance:**

1. An unauthorized task is shown as blocked with its exact scope ID and next step;
   coverage scope is explicitly distinct from the network grant.
2. After an operator updates/reloads policy, refreshing status explains readiness;
   a real tool request still performs authoritative admission, preventing races.
3. Unreachable checks show unknown/unavailable, never ready; custom Python testing
   reports the worker's interpreter/capability, not the API image's absent Python.

**Benefit/measure:** fewer failed first runs and support-log searches; target
operator diagnosis within two relevant UI steps, measured on synthetic setup cases.
**Dependencies/sequence:** agree a bounded read-only broker status API first; share
its state model with task UI. **Effort:** Medium because this spans the private
broker and frontend while preserving authorization. **Open questions:** which
operator details may be shown remotely, and is a read-only CLI status sufficient
for the actual user population? Confirm in first-run sessions.

### PR-02 — Include retest history in portable results

**Classification:** confirmed gap. **User/job:** the assessor and report recipient
need to understand why a finding is fixed, still reproducible or inconclusive.
**Evidence:** [operations](OPERATIONS.md) states the omission; the table list in
[db/task_archives.go](../db/task_archives.go) lacks `finding_retests` and its separate
conversations; [report Input](../report/report.go) has no retest field. The real
lifecycle/deduplication is tested in [server/finding_retests_test.go](../server/finding_retests_test.go).

**Proposed scope:** version the task package and supported Markdown/JSON exports
to carry retest attempts, verdicts, conditions, source evidence references and
relevant conversation evidence. Show the latest result and preserved attempt list
in the recipient output. Preserve current older-package restore support.

**Permissions/rules/edges/exclusions:** only the operator exports; include only
the selected findings/task, respect holds, evidence redaction and retention.
A missing/restarted/stopped attempt is not a successful fix. Do not infer fixed
from absence or export unrelated conversations. PDF styling and client portals
are excluded.

**Acceptance:**

1. Fixed, reproducible, inconclusive, stopped and failed attempts survive export
   and archive/restore with stable parent references and no duplicate attempts.
2. An older package restores with an explicit “retest history not included” state.
3. Missing ephemeral/raw traffic is disclosed; retained finding evidence and the
   retest's own conclusion are never silently replaced by an empty success.

**Benefit/measure:** recipient can trace every exported fix claim to its attempt;
measure complete provenance for all attempts in a seeded five-outcome fixture and
reduced manual reconstruction time. **Dependencies:** archive schema/ID remapping,
export redaction and current evidence controls; deliver after storage follow-up.
**Effort:** Large due to schema evolution, copied evidence and compatibility tests.
**Open questions:** required handoff fields, whether retest conversation text may
contain client secrets, and who approves redaction. Validate with report recipients.

### PR-03 — Honest failures and safe committed-task recovery

**Classification:** confirmed interface gap; duplicate creation is a realistic
consequence not reproduced here. **User/job:** operator distinguishes “no records”
from “could not load,” and repairs a partially created task without repeating work.
**Evidence:** a mocked-fetch probe confirmed [api.ts](../web/src/lib/api.ts) retains status in `ApiError` but drops `created`, `task_id` and attachment recovery fields;
[task form](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/tasks/page.tsx) uses a generic toast.
The backend's [staged-copy test](../server/staged_uploads_test.go) returns an existing
paused task explicitly. [Commands](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/commands/page.tsx)
clears a failed read to `[]`; [Model recordings](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/llm-records/page.tsx)
ignores load errors while retaining prior state. [ReportTab](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/tasks/detail/_tabs/report-tab.tsx)
already provides a useful loading/error/retry pattern. A disposable Commands
component probe confirmed the read failure is displayed as the empty state.

**Proposed scope:** extend the existing `ApiError` to preserve bounded safe domain fields;
creation recovery links to the committed task and attachment repair. Read views
represent loading, empty, stale and failed distinctly with explicit retry. Add
idempotent retry identity for task creation to handle lost post-commit responses.
Limit this to task creation and the two evidenced read views first.

**Permissions/rules/edges/exclusions:** same authenticated operator; never expose
arbitrary diagnostic/secret response fields. Scope request identity to the create
operation. Preserve drafts, handle response loss, double submit, refresh and a
quota-full repair. Do not automatically resume execution or clear a legal hold.
General idempotency for every API is excluded.

**Acceptance:**

1. A `created=true`/507 response opens or links the existing paused task, preserves
   the draft recovery context and does not offer an unnoticed second creation.
2. Replaying one create request ID after a lost response returns the same task;
   a deliberate new task uses a new identity and is visibly distinct.
3. Commands/model-record failures show an accessible error/retry state; old rows
   may remain only with an explicit stale indication. A genuine empty result is
   separate. Retry success clears the error without changing the filters.

**Benefit/measure:** zero unintended duplicates in post-commit drop/double-submit
fixtures; all injected read failures classified correctly. Measure recovery time
and duplicate-task reports. **Dependencies:** client error contract, create storage
and state tests before visual polish. **Effort:** Large across transaction identity,
client transport and several states. **Open questions:** safe recovery identifiers
and idempotency expiry; inspect actual support cases before extending scope.

### PR-04 — Storage status and hold recovery in the product

**Classification:** confirmed UI gap. **User/job:** the operator safely resolves a
full bucket or held deletion without treating it as a mysterious failed upload.
**Evidence:** [server/storage.go](../server/storage.go) implements status, hold and
purge routes; [storage tests](../server/storage_test.go) verify confirmations and
507 admission. No storage route/client/view was found in reviewed frontend
navigation/API. [Evidence guide](EVIDENCE-STORAGE.md) currently documents curl.

**Proposed scope:** one operator storage panel with bucket usage/limits, capture
maintenance errors, hold reasons/IDs and raw-data lifetime. Contextual links from
quota/hold failures lead there. Expose the existing confirmed purge/hold-release
workflows with explicit affected scope and safe previews.

**Permissions/rules/edges/exclusions:** operator only; model tools cannot create
or release holds. Distinguish referenced finding evidence from expiring raw data
and live ephemeral SDK files. Disable unavailable actions with reasons; handle
full buckets, corrupt hold metadata and another tab changing state. No automatic
hold release, database row browser or claim to immutable compliance storage.

**Acceptance:** status matches API counters; a held purge explains the hold and
changes nothing; a confirmed unheld purge reports its bounded result and a retry
can recover a failed upload; database/log/backup capacity is displayed as separate
scope rather than included in file-quota percentages.

**Benefit/measure:** fewer manual commands and mistaken deletions; measure correct
hold/TTL understanding in five seeded scenarios and recovery steps per quota
incident. **Dependencies:** PR-03 safe errors and final PR-07 scope contract.
**Effort:** Medium, reusing existing APIs with sensitive confirmations.
**Open questions:** prefer a built-in panel or an operator CLI? Validate workflow
frequency before expanding beyond status and existing safe actions.

### PR-05 — Keyboard-complete execution and evidence review

**Classification:** confirmed narrow keyboard entry-point gap, broader responsive
usability unverified. **User/job:** operator or assistive-technology user opens,
reads and closes details without a mouse. **Evidence:** Commands and Model
recordings rows use `onClick` without a button or keyboard/tab handler; the
[FindingsTable](https://github.com/jaysonguglietta/artexV2/blob/e30fe097a3946d17ab63ac7cf540be1e0efce401/web/src/app/%28main%29/function/findings/_components/findings-table.tsx)
already implements Enter/Space, focus and expansion semantics. A disposable
Commands component probe confirmed click opens details while Enter does not.
This is not a full real-browser or screen-reader assessment.

**Proposed scope:** semantic, named detail triggers for those two tables; focus
return and keyboard drawer navigation. Cover the core task/findings/approval/file
journey with browser keyboard and responsive tests, reusing existing components.
Do not redesign typography or all charts merely for consistency.

**Permissions/rules/edges/exclusions:** opening details preserves existing auth;
row selection/copy/actions must not trigger unintended opening/deletion. Handle
long payloads, paginated rows, loading replacement, empty results and a selected
record disappearing. Graph interactions may use an equivalent accessible list;
full graph-editor redesign is excluded.

**Acceptance:** keyboard alone opens/closes a selected command/model detail and
returns focus; screen-reader names/states are meaningful; 375/768/1280-pixel views
permit filters, detail inspection and action confirmations without overlap or
trapped focus; automated checks plus a manual screen-reader pass cover core paths.

**Benefit/measure:** all critical review actions complete without a pointer in
acceptance fixtures; track keyboard task completion and accessibility regressions.
**Dependencies:** browser harness and PR-03 states. **Effort:** Medium due to focus,
responsive and assistive checks across composed controls. **Open questions:** actual
operator device/assistive needs and supported browser matrix; test with users.

### PR-06 — Verified whole-installation backup and recovery

**Classification:** baseline production-readiness gap; **authorized work in
progress**, not an unimplemented promise. **User/job:** VM operator can recover
needed assessment state after loss/upgrade without corrupting the source system.
**Evidence:** archive round trips cover task packages, not the entire database,
keys, evidence and broker policy/ledger. [Operations](OPERATIONS.md) instructs
backups but baseline recorded validation has no complete restore proof. The current
workstream plans `scripts/artex-backup.py`, `scripts/test-backup.py` and
`docs/POSTGRES-BACKUP.md`; these must get their own evidence before status changes.

**Proposed smallest complete scope:** encrypted, versioned manifest of the stated
backup scope and a disposable restore verification command. Define quiescing or
consistency guarantees across PostgreSQL, raw SQLite/files and required private
state. Report omitted ephemeral data and protect key material separately.

**Permissions/rules/edges/exclusions:** explicit operator authority; no overwrite
of live/source volumes. Fail closed for incomplete manifests, wrong decryption,
corruption, disk exhaustion and incompatible versions. Preserve legal-hold state;
review replay/session/credential rotation after point-in-time restoration. No HA,
replication, cloud backup service or forensic-atomic promise.

**Acceptance:** a seeded backup restores to a new isolated deployment with expected
task/finding/evidence hashes and hold state; corruption and missing keys fail before
activation; the original deployment is untouched; recovery instructions identify
fresh credentials/state needed and prove actual read/use after restore, not just
that an archive decrypts.

**Benefit/measure:** measured restore time and completeness for a fixed fixture;
agree RPO/RTO with the operator rather than inventing them. **Dependencies:** PR-07
capacity, storage scope and existing archive/key/broker rules. **Effort:** Large
because consistency, encryption, migration and restore verification span services.
**Open questions:** required recovery point/time, who holds decryption keys, backup
location/retention and acceptable quiescence. Coordinate the active workstream.

### PR-07 — Actual PostgreSQL and log capacity boundaries

**Classification:** confirmed scope limitation at baseline; **authorized work in
progress**. **User/job:** operator prevents a long assessment or log burst from
filling the VM outside the existing file buckets. **Evidence:** [storage status](../server/storage.go)
explicitly excludes PostgreSQL/WAL, logs and image layers; [Compose](../docker-compose.yml)
and worker creation define resource limits but do not establish a host-wide disk
cap. Current work investigates explicit physical PG capacity and Docker/journald
policy; unsupported volume `size` options must not be represented as hard limits.

**Proposed scope:** document/enforce the supported-host database boundary, rotate
all relevant container/per-run logs, and expose actionable low-space/refusal state.
Preserve existing volume migration as a deliberate, reversible operator operation.

**Permissions/rules/edges/exclusions:** host administrator controls privileged
volume/log policy; no new mount authority in API/workers. Existing data is never
silently reformatted. Handle PG/WAL/index growth, full logs, unsupported filesystems,
restart and backup headroom. Separate admission estimates from physical hard caps;
no promise to limit every other process on the host.

**Acceptance:** dedicated fill tests demonstrate the claimed actual cap and
bounded log rotation; application/database failures stay explicit and authenticated
read/recovery behavior is documented; restart retains policy; an unsupported host
is refused or clearly labeled outside the supported hard-cap mode. Existing source
volume remains available through an explicit migration/rollback test.

**Benefit/measure:** bounded declared bytes under data/log flood, and observable
recovery without collateral source-volume loss. **Dependencies:** approved host
architecture, PR-06 verification and capacity thresholds. **Effort:** Large because
actual storage guarantees are platform-dependent. **Open questions:** supported
Linux/Colima hosts, operator root access, total disk reserve and backup destination.
Do not close this based only on a YAML option or successful startup.

### PR-08 — Complete release posture and image-finding disposition

**Classification:** baseline hosted-operation limitation plus **authorized work in
progress**. **User/job:** maintainer produces a reviewed release with enforced tag
ownership and an honest remaining vulnerability record. **Evidence:** [image guide](IMAGE-SECURITY.md)
and [signed guide](SIGNED-RELEASES.md) implement strict gates and a real main-only
rehearsal. [Validation](VALIDATION.md) records Medium/Low findings and no production
version-tag release. Source assertions do not prove hosted tag rules are active.

**Proposed scope:** finish current package/base/tool remediation and primary-source
disposition, verify hosted semantic-tag protections, consolidate stale updater
wording in Operations with the canonical signed-release guide, and retain exact scan/signature
proof for the selected commit/architectures. Keep public source pushes distinct
from release activation.

**Permissions/rules/edges/exclusions:** repository owner/approved release identity
controls rules and opt-in; no broad tag bypass or mutable overwrite. Treat active
findings, withdrawn advisories and explicit risk decisions separately. Failed or
unavailable verification blocks release. No blanket suppression, fabricated fixed
version or weakening a gate to obtain a green badge.

**Acceptance:** each previously remaining Medium/Low issue has evidenced patch,
removed-unused-component or precise reviewed residual disposition; raw counts stay
visible. Negative hosted checks demonstrate unauthorized tag create/move/delete
refusal and approved release behavior. Exact artifact identities/gates/signatures
match the release and partial-publication recovery is explicit. Do not claim all
issues fixed if a residual remains.

**Benefit/measure:** no unreviewed findings in the selected release inventory and
zero accepted unauthorized tag mutation cases. **Dependencies:** active root/server
work, administrative repository access and current CI. **Effort:** Medium relative
to existing pipelines; hosted settings/retests are the remaining integration.
**Open questions:** authorized tag creators/bypass policy and residual-risk owner.
A production tag exercise requires its own authorization; rehearsal is not one.

### PR-09 — Repeat-assessment delta view (research first)

**Classification:** product hypothesis, not a confirmed requirement. **User/job:**
a repeat assessor may need to distinguish new, changed and no-longer-observed
results without spreadsheet reconstruction. **Evidence/motivation:** source-task
relations and reusable templates already exist; findings can be filtered/exported
by task, but no explicit paired-task delta view was found in inspected navigation.
Today the operator can compare separate exports and retest individual findings.

**Validation before build:** observe three to five representative repeat-assessment
handoffs. Continue only if comparison is frequent and manual reconciliation is a
material cost; otherwise retain existing filters/export/retest rather than adding
another dashboard.

**Smallest proposed scope if validated:** compare two operator-selected completed
tasks using explicit stable finding/asset keys, expose matching confidence and
allow correction. Show new/changed/unchanged/not-observed; link original evidence
and PR-02 retest outcomes.

**Permissions/rules/edges/exclusions:** one operator's authorized tasks only. No
“fixed” inference from absence, merged identity across unrelated customers or
automatic target grant. Handle scope changes, duplicate endpoints, severity edits,
partial tasks and missing retained evidence. Exclude scheduled recurring scans,
team ownership, predictive scoring and broad BI.

**Acceptance:** seeded matching/ambiguous/scope-change fixtures produce transparent
classifications; unmatched/partial evidence never asserts remediation; a corrected
match is persisted and explainable. **Benefit/measure:** propose a 25% reduction in
observed repeat-report reconciliation time, subject to a measured baseline.
**Dependencies:** research gate, matching semantics, stable exports and PR-02.
**Effort:** Large due to ambiguous identity and evidence provenance, not chart work.
**Open questions:** repeat frequency, receiver needs and acceptable false matches.
Confidence remains Low until the validation step is complete.

## 16. Features to Simplify, Consolidate, or Remove

- **Consolidate runtime capability guidance (PR-01).** The API's Python detector and
  saved path do not configure the DB-free worker interpreter. Replace the misleading
  authority with worker-specific status; migrate/preserve legacy configuration
  deliberately. Do not remove the actual Python tool capability.
- **Consolidate failure/state patterns (PR-03).** Reuse ReportTab's explicit
  loading/error/retry model in Commands and Model recordings, while retaining the
  distinct purpose/data of tool traces versus model wire records.
- **Consolidate keyboard detail triggers (PR-05).** Reuse the good Findings pattern
  rather than inventing a different row interaction per table.
- **Consolidate release instructions (PR-08).** The baseline Operations guide
  still describes in-process updates as disabled by default/upstream conveniences,
  whereas current production refuses them permanently. Use the signed-release
  guide as the canonical installation authority, retaining historical behavior
  only where explicitly labeled.
- **Keep the read-only release card.** Hiding unavailable apply/rollback actions
  with signed-install guidance is already implemented/tested; it is not a new gap.
  Removing the old disabled code path entirely needs an explicit compatibility
  decision and is lower value than the core backlog.
- **Keep intentional distinctions.** Do not merge asset versus exploration graphs,
  raw TTL versus retained finding evidence, task archive versus full backup, or
  synthetic preview versus real execution. These solve different jobs.
- **Do not remove an unverified integration solely for lacking live credentials.**
  ScopeSentry/provider/notification support needs scoped acceptance evidence, not
  arbitrary deletion. Likewise, preserve exact historical evidence and protocol
  lookup data rather than deleting it for language-count cleanliness.

No extra simplification epic is justified: the specific changes are already
owned by the deduplicated backlog items above.

## 17. Recommended Delivery Sequence

1. **Release blockers:** none newly demonstrated by this product review. Keep
   actual security gates, supported deployment constraints and production release
   opt-in authoritative; do not turn every P1 recommendation into a P0.
2. **Finish authorized operational work:** PR-07 architecture/capacity → PR-06
   encrypted disposable restore proof; PR-08 image remediation/tag protection can
   proceed alongside them. Update their statuses only from concrete evidence.
3. **Complete core journeys:** PR-01 first-scan readiness and PR-03 safe failure
   recovery share runtime/error contracts. Then PR-02 makes retest evidence portable.
4. **Production usability:** PR-05 keyboard paths and browser acceptance accompany
   those flows; PR-04 exposes the finalized storage/capacity contract without
   duplicating an unstable policy surface.
5. **Validated enhancement:** run PR-09 research before committing implementation.

There are no calendar estimates: delivery capacity, operator availability and
actual-host constraints are unknown. Effort is relative, not a schedule promise.
Proposals outside the four authorized workstreams remain recommendations only.

## 18. Product Validation and Acceptance Test Plan

The role model is unauthenticated client versus one trusted operator, plus
untrusted worker/model/target data. Test external report recipients through an
exported artifact, not a nonexistent app role. Keep all automated targets and
receivers local/synthetic; no real scanning, emails or provider charges are needed
for the default acceptance suite.

| Journey/area | Observable acceptance and failure/recovery checks |
|---|---|
| First visit and first task | Setup-token/password initialization, failed login/throttle, reload/logout, configure synthetic provider, create task, inspect exact readiness, grant only its approved CIDR externally, resume and produce a seeded finding. A denied/unreachable worker must not appear ready. |
| Create/retry/upload | Required/duplicate/stale inputs; combined file budget; copy failure after DB commit; lost response/double click; existing paused task recovery; reload draft context; no automatic duplicate or scan resumption. |
| Routine control | Queue/concurrency, pause/resume, timeout, exhausted/failing model chain, interrupted stream and service restart; task status reflects durable/observed state; cancellation leaves a renewed sibling alive. |
| Findings/retest/handoff | Edit severity/status, change evidence and show stale report, retest every outcome, double-start deduplication, export selected/filtered findings, archive/restore, and verify PR-02 provenance. Inconclusive/absent evidence never becomes fixed. |
| Failure honesty | Inject 401/403/409/507/timeout/500 into the two record views and task creation; distinguish empty, stale and failed; retain filters; accessible retry and safely bounded diagnostics. |
| Files/retention/holds | Quota refusal leaves originals intact; held delete/purge refuses without side effects; 30-day raw expiry leaves retained finding evidence; live SDK tmpfs is not claimed held/durable; malformed hold state fails closed. |
| Integration | Synthetic local provider/tool schema, MCP transport, import schema variants, notification filters/digest/retry/deep links; actual external account delivery/ScopeSentry compatibility requires a separately authorized, documented check. |
| Recovery/capacity | On a disposable VM, fill the actual supported PG/log boundary, restart, create encrypted backup, verify restore to fresh storage, corrupt/remove components and prove refusal while source stays untouched. Record required credential/ledger/hold handling. |
| Release operation | Current exact image scans, primary-source residual dispositions, hosted rule negatives, approved tag policy, signature identity/config/rootfs binding, missing/altered assets and safe partial-publication recovery. No real release is implied by local fixtures. |
| Keyboard/accessibility | Only keyboard: navigate task list, open command/model details, edit a finding, inspect approval, upload/download, copy/export and exit dialogs. Check named fields/buttons, announcements, visible focus, focus return, contrast and a manual screen-reader run. |
| Responsive behavior | At 375/768/1280 widths: long names/payloads, dense tables, drawers, tabs, scroll and fixed headers; no hidden critical action, overlapping text or impossible confirmation. Test relevant touch input without assuming a mobile-app redesign. |
| Concurrency | Two tabs editing a finding/profile/task, retrying create/retest, releasing a hold during a mutation and deleting the selected record; stale state is disclosed and cannot silently overwrite a more recent evidence version. |

For volume tests, first agree VM/browser resources and latency budgets. Seed
representative single-operator sets, initially 100 tasks, 50,000 assets, 5,000
findings and 100,000 activity records within declared storage limits. Measure API
p95/payload size, query count, initial rendering, pagination and long-running
memory; do not claim those proposed volumes are already supported because a list
uses pagination. Include an empty dataset and the configured quota boundary.

Browser tests must use the real frontend/backend path rather than only mock mode.
Retain DOM component tests for state contracts, API/database tests for business
rules and disposable Docker tests for actual runtime behavior; no one layer proves
all the others. Each delivered backlog item should attach evidence against its
specific acceptance criteria. User research is required for PR-09 and useful for
PR-01/04; no product research interviews were performed in this review.

## 19. Product Open Questions and Assumptions

1. Is one technically capable operator the lasting target? Shared tenants/users
   would require a new security/product model and would materially reorder scope.
2. What is the first meaningful successful assessment: a training target, internal
   app, imported asset estate or a specific target protocol? Real model quality,
   authorized coverage and cost ceilings need a controlled success fixture.
3. Which provider/integration versions and browser/assistive environments must be
   supported? Live compatibility has not been inferred from synthetic fixtures.
4. How often are retests and repeat reports handed to another person, and what
   evidence may that person receive? This determines PR-02 fields and PR-09 value.
5. What RPO/RTO, backup recipients/retention and actual Linux/VM privileges are
   available? PR-06/07 are in progress and must not close on untested assumptions.
6. Which hosted actors may create release tags, who owns residual image risk and
   what is the authorized production release process? Main-only rehearsal is not
   protected-tag publication proof.
7. Are out-of-band policy/hold commands acceptable for repeat operation, or should
   status/recovery be in the UI? PR-01/04 deliberately preserve private policy
   authority; convenience must not widen model capabilities.
8. What measured scale and support incidents justify further work? The review did
   not collect user telemetry, support histories, real task datasets or interviews.
9. New capacity/backup/tag/image code is being developed concurrently. Its final
   tests and docs belong to the current workstream and root-owned validation record;
   this baseline review does not pre-approve or certify those changes.

The five highest-value next actions are:

1. **Finish actual-host capacity proof (PR-07)** so database/log growth cannot
   bypass the declared operating envelope; it is already authorized work.
2. **Complete encrypted disposable recovery (PR-06)** against that envelope,
   verifying usable data and protected source volumes rather than decryption alone.
3. **Close release posture with concrete hosted/image evidence (PR-08)** while
   retaining honest raw findings and the distinct production-tag trust boundary.
4. **Validate first-scan readiness and recovery contracts (PR-01/03)** with an
   operator before authorizing UI changes; these address the strongest core
   workflow friction evidenced in code.
5. **Agree the retest handoff contract (PR-02)** with a report recipient before
   changing archive/export formats; it prevents a portable “fixed” claim from
   losing its supporting attempt history.

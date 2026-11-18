# Upstream changelog

This file records important upstream changes in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. These are translated historical release notes, not current deployment instructions. Current worker isolation and security behavior supersede older defaults; see the main README and secure-deployment guide.

## [Unreleased]

### Interception

#### Added

- **Added a built-in deletion-endpoint path rule.** Earlier destructive HTTP rules recognized DELETE methods (`curl -X DELETE`, `requests.delete(`, `method:'DELETE'`), while the path rule covered only `/clear /wipe /flush /purge /truncate /drop /destroy /factory-reset /reset-all`. GET/POST endpoints such as `/api/user/delete?id=1` therefore escaped those rules. A separate deny seed now covers `/delete /del /remove /unlink /erase /destroy`, including delimited suffixes such as `/deleteAll`, `/delete_user`, and `/delete-user`, while avoiding `/delivery`, `/details`, `/delta`, and `/delegate`. Its independent seed marker also updates existing installations. Like other built-ins, operators can disable or delete it in Command interception.

### Finding notifications

#### Added

- **Finding notifications support six channel types:** DingTalk, Feishu, WeCom, generic webhook, Telegram, and email. Multiple instances of a kind can have independent enablement, rates and filters.
- **Realtime and digest delivery.** Realtime sends each event; digest combines findings over the global interval, initially 30 minutes, with severity counts. Separate channels can implement high-severity realtime plus lower-severity digest policies.
- **Four filter dimensions:** minimum severity; task/asset scope; included/excluded finding-type keywords, with exclusion first; and optional disposition changes, off by default. Optional detail links use the configured public backlink base.
- **Delivery history and manual retry.** Filter by channel/state and inspect attempts/reasons. Manual retry resets attempts because it assumes the operator addressed the cause.
- **Masked credential reads.** Each channel declares secret fields through `SecretKeys()`. The UI receives a suffix-hinted mask; returning it unchanged preserves the stored secret, while explicitly clearing a field removes it.

#### Fixed

> These entries describe the security review after the feature's first implementation. Each issue was reproduced, fixed, and covered by a regression test.

- **Closed credential-preservation destination pivots (severe).** Changing a webhook/Telegram/SMTP destination while omitting credential fields formerly preserved real stored credentials and sent them to the new destination. Reproductions covered four channels. Destination changes now require an explicit new value or explicit empty value for every credential; unchanged masks are rejected too. Credentials are not silently discarded, because optional authentication disappearing behind a success response would be difficult to diagnose.
- **Escaped untrusted Markdown.** DingTalk, WeCom and Feishu formerly rendered model-generated titles/summaries and target-controlled URLs without escaping. An injected external link or image beacon could mislead readers or expose their IP/read activity. Each Markdown renderer now flattens lines and escapes its metacharacters. Escaping stays at the format-specific output boundary so Markdown backslashes do not leak into Telegram HTML.
- **Hardened delivery SSRF at dial time.** Scheme/host validation alone permitted metadata, loopback and internal responses whose first 200 bytes appeared in delivery errors. Dial-time checks cover DNS rebinding and same-host redirects; cross-host redirects are rejected to avoid forwarding URL credentials. Loopback/link-local require explicit `ARTEX_NOTIFY_ALLOW_LOCAL=1`. RFC1918 remains intentionally available for private Mattermost/SMTP deployments; this is not a general private-network sandbox.
- **Redacted destination credentials in errors.** URL errors could expose DingTalk access_token, WeCom key, Feishu hook IDs or Telegram bot tokens through stored last_error, history, logs and test-send responses. Errors now retain only scheme/host and the underlying diagnostic, dropping path/query. URL-parse failures receive the same treatment; a regression was added after earlier tests failed to cover that branch.
- **Prevented silent loss after digest truncation.** Only complete items that fit a channel message are marked delivered; remaining items return to the queue with an honest displayed/deferred count. Deferral refunds the claim attempt, so a 500-item backlog is not falsely exhausted before later segments are ever sent.
- **Validated minimum severity on writes.** Misspellings such as hgih formerly mapped to rank zero and silently disabled filtering. Save errors now list accepted values; reads remain tolerant of existing malformed configuration.
- **Made rate_per_min=0 reachable.** The storage layer no longer replaces explicit unlimited zero with 20/100. The API supplies defaults only when the request field is omitted.
- **Applied rate limits to digest delivery.** Previously consumed token allowances were unused. Digest planning now respects the send budget independently of bounded batch memory.
- **Evaluated retry exhaustion per delivery.** An old exhausted row no longer permanently fails fresh rows in the same digest. Permanent errors fail immediately; other rows use their own attempts/backoff.
- **Explicitly failed malformed digest snapshots.** Skipped rendering entries are no longer included in a blanket success update, and delivery history records their failure reason.
- **Bounded serial sends by the claim lease.** A three-minute lease and per-send timeout require a conservative per-channel tick budget. The initial value six left no margin; it was reduced to five and a regression pins the relationship.
- **Preserved Telegram HTML entity boundaries during truncation.** Avoid unfinished entities as well as tags so a long message is not rejected entirely.
- **Retried transient SMTP failures.** 4xx responses, including graylisting 450, are retryable; 5xx are permanent; absent codes default to retryable.
- **Rejected nested mask sentinels.** Object-valued fields such as webhook.headers must be masked or supplied as a whole. A mask nested inside an object cannot mean unchanged and must not become a literal stored credential.

#### Design notes

- **Finding writes perform one best-effort event INSERT in the same transaction.** The INSERT reads neither channels nor user filters. A SAVEPOINT isolates a notification statement failure, because otherwise PostgreSQL aborts the entire finding transaction. Failure is logged without losing the finding.
- **Delivery claims use leases, not network-spanning transactions.** `FOR UPDATE SKIP LOCKED` claims a row, marks sending, and moves next_attempt_at to lease expiry before committing. Network delivery holds no DB lock. Crashed sending rows are reclaimed after expiry within a finite retry budget.
- **Rate limiting does not spend retries.** Compute each channel's token allowance before claiming rows. Claiming then discarding rate-limited work would exhaust its attempts while merely waiting; excess work instead waits for a later tick.

### Test infrastructure

#### Fixed

- **Fixed leaked test assets caused by cleanup ordering.** A company-ownership fixture registered database cleanup with t.Cleanup but closed its connection using defer, which ran first. Ignored cleanup errors left assets/companies behind. Its fake TaskID based on MAX(company.id)+1 could collide with other tasks and break exact-count tests. Connection closure now also uses t.Cleanup, registered first so LIFO cleanup runs before it, and cleanup errors are surfaced. Other similar fixtures were noted; this change fixed the demonstrated instance.

### Traffic

#### Added

- **Added Clear all traffic.** It ignores filters, deletes indexed exchanges and leftover host directories, then runs optimize, VACUUM and wal_checkpoint(TRUNCATE), reporting released space. Independently stored finding evidence remains intact. Clearing also converts an existing empty index to incremental auto-vacuum so subsequent host deletions reclaim space.

#### Fixed

- **Reclaimed disk after traffic deletion.** SQLite freelist pages did not shrink the old index, and contentless-delete FTS tombstones accumulated without merging. Inline bodies below 256 KiB plus trigram indexing amplified storage; one measured 6 MiB body sample kept a 16 MiB index after deletion. New indexes use auto_vacuum=incremental. Background chunks merge FTS, run incremental_vacuum and checkpoint the WAL, releasing locks between chunks and yielding on shutdown. The measured empty index fell to 104 KiB; unfinished work continues on a later deletion.

> Upgrade note: existing indexes keep their old auto_vacuum mode, so incremental_vacuum alone is a no-op and startup reports it. FTS merging still stops tombstone growth. Clear all once to reclaim the old footprint and switch that index to incremental reclamation.

### LLM

#### Fixed

- **Applied custom session headers to one-shot model calls.** agentcore formerly set the session context only with transcript storage, so goals round zero and cold-digest body calls lacked it. Gateways requiring x-opencode-session returned MissingSessionID, while ordinary planner rounds worked. Explicit exploration-stable IDs (`exp<N>-goals` and `exp<N>-compactor`) now supply the header and allow llmrec to attribute their previously missing usage.

### Findings

#### Fixed

- **Restored scrolling in the findings asset tree.** A parent max-height with automatic height could not resolve a child height:100%, causing overflow or clipped unscrollable rows. The native tree scroller now owns overflow-y-auto and a viewport-aware max-height; short trees shrink while long trees scroll.

## [0.3.14] - 2026-09-24

### Task list

#### Added

- **Added finding counts to the task list**, grouped and colored by Critical/High/Medium/Low severity.

#### Changed

- **Narrowed description/goal columns.** Ellipsis plus full hover text keeps long task descriptions from consuming the table width.

### Exploration graph and planning overview

#### Changed

- **Bounded graph_overview lists.** Recent facts, terminal/open intents, cold digests and confirmed findings now return a latest window plus total counts, with on-demand queries for omitted items. Related-task digests are bounded too, reducing long-task context growth.

#### Fixed

- **Removed duplicate reverse digest-to-finding edges** after folding cold nodes.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset interception

#### Added

- **Added global asset block rules** for exact/partial domain, IP and URL matches plus CIDRs, with CRUD and enablement under Asset interception. Default seeds match government (.gov/.gov.cn) and education (.edu/.edu.cn) domains.
- **Checked asset rules before add_intent and insert_assets.** Blocked intents are not dispatched; blocked assets are not inserted; the agent receives the asset and reason.
- **Added task-level block and allow rules.** Deny wins; if allow rules exist, a candidate matching none is refused. No allow rules means no task allowlist. Rules can be configured at creation or in task overview.
- **Task templates include category and task rules**, applied together to the creation form.

### Action review

#### Fixed

- **Shortened the model-review output contract.** Reduced explanation length from 500 to 120 characters and required JSON without preamble/fences, reducing truncation/parsing failures that could trigger a historical fail-open policy.

### Task archives

#### Fixed

- **Skipped archive symlinks instead of failing the whole package.** Archives support regular files/directories only; symlinks are logged and omitted without following them outside the tree.

### Account and authorized use

#### Added

- **Added an authorized-use/disclaimer dialog before login**, requiring explicit acknowledgement.

### License and dependencies

#### Changed

- **Adopted AGPL-3.0** and clarified README license/disclaimer notices.
- **Updated norma to v0.4.1.**

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration broadcast

#### Added

- **Added the exploration broadcast timeline** (#144): task nodes of all kinds with type/text filters, sort direction, pagination and automatic refresh. Newest-first page one is live; elsewhere polling only increments unread counts, with a return-to-latest action. Day groups retain date context in long tasks.
- **Displayed node IDs** (#145) for graph correlation.
- **Expanded upstream/downstream/asset details** (#147): hover cards include kind, status, source, time, summary and payload excerpt. Neighbor data arrives with the page, avoiding extra expansion requests.
- **Added exact node-ID search** (#150), accepting numeric IDs and displayed #41 syntax.

### Intent management

#### Added

- **Added soft/hard intent deletion** (#149) for pending, running and paused intents, requiring a reason. Soft deletion (default) keeps the node, output and lineage with state deleted. Hard deletion removes the intent and exclusively supported descendants through intent/output chains, keeps dated token accounting, preserves shared nodes/goals/root facts, and previews cascade counts.

  Both modes notify the planner of the user deletion and its reason for replanning.

### Action review

#### Added

- **Filtered review history by status and decision source** (#139).

#### Fixed

- **Loaded pending approvals independently of history pagination** (#133), keeping every pending request visible while browsing history.

### Agent

#### Changed

- **Generalized the worker role description** to cybersecurity-platform wording (#138).

#### Fixed

- **Enabled previously unwired cold-node compaction** by attaching the Compactor to task planners.

### Chat

#### Added

- **Added paginated multi-record @ references in chat** (#135).

#### Fixed

- **Kept long chat bubbles inside their session panel** (#137).
- **Fixed uploads before a draft conversation exists.** Composer now snapshots FileList into an array before clearing the input. Previously asynchronous conversation creation resumed after the live FileList had been emptied, omitting the upload file and causing HTTP 400.

### Traffic

#### Fixed

- **Bound the recording proxy to 127.0.0.1 by default** (#129, #130), avoiding an unintentionally exposed proxy.

### Web

#### Fixed

- **Restored the global header on the statically exported task list.**
- **Added missing demo finding-evidence mocks to fix the blank detail page.**

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Action review

#### Added

- **Linked approvals to the exact source call** (#125), loading the complete original conversation and enough pages to expand and center-highlight its command/result. Manual scrolling stops automatic correction. Persistent tool_use_id plus task mapping and scoped indexes support ordinary chat, worker, planner and segmented main-agent sessions. Missing, duplicate or ambiguous matches produce an explicit notice rather than another call; deleted conversations and archived/missing records are reported clearly.
- **Paginated approval history** (#115).

#### Changed

- **Reduced model-review input** (#124, #125) to versioned JSON containing the complete current call, explicitly selected short background and local working directory. Background comes only from the actual current user message in chat/main-agent; workers discard inherited background and intent summaries, while planner/scheduled runs fabricate no user message. Task description/goals/constraints/overview/history/full intent remain agent execution or audit context but are excluded from review. Verdict JSON requires decision/comment explaining the operation, consequence and applicable rule. Instructions inside arguments/background cannot change review policy. Persist the actual input snapshot/fingerprint and retain versioned old snapshots without reconstructing them from current data.

#### Fixed

- **Handled fenced verdict JSON before strict parsing** (#126), so a correct DENY wrapped in a code fence no longer fell through the historical model-failure policy.

### Agent

#### Added

- **Added experimental noa context compression** with norma v0.4.0, off by default. It replaces built-in compaction for main-agent/planner/worker/chat and archives originals under `<workDir>/noa/<sessionID>/`, centrally rather than per task. Integration failure falls back to built-in compression without interrupting the task. Read the switch once per run so changes affect later runs.

#### Fixed

- **Rejected graph tools without task context** instead of dereferencing a nil store.

### MCP

#### Added

- **Supported legacy SSE MCP servers** (#117).

### Traffic

#### Fixed

- **Made traffic_search host matching port-aware** (#114), avoiding interference between different ports on the same host.
- **Isolated traffic_search-description migration failure** so it no longer prevents subsequent reporter migrations.

### Web

#### Changed

- **Separated retest finding choices visually** (#122).

#### Fixed

- **Fixed demo task-detail crashes.** The sessions tab requested `/api/tasks/<id>/side-questions`, but the mock's plural-collection fallback returned [] instead of an items object. The resulting undefined iteration failed inside a React state updater, escaped the caller's catch and reached the page error boundary. Added an explicit empty mock history response and normalized non-array items to [] in sideAPI.history as defense in depth.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Network

#### Added

- **Added DeepSeek official web search**, reusing the active LLM profile. This is a server-side `web_search_20250305` tool inside DeepSeek's Anthropic-compatible endpoint, not a standalone search API, so only the official endpoint with Anthropic format is supported. Each search consumes an extra model call, bypasses the separate search proxy and traffic recorder, and returns titles/links without summaries; WebFetch can retrieve bodies. Settings explain these limits without preemptively blocking configuration; the test-search action verifies actual availability.

### Agent

#### Added

- **Added cross-work trace review for workers.** search_all_worker_traces finds matching task steps without first knowing intent_id; get_worker_trace lists/searches steps or fetches complete step IDs. This reuses observations omitted from facts and avoids repeated work.
- **Exposed node_detail to workers** for IDs learned through trace review.
- **Made add_hint planning triggers explicit.** One batch creates one planner notification identifying the new strategic hints, rather than relying on the planner to notice them in the overview.

#### Changed

- **Relaxed overview prompt tone** to encourage exploration and timely cross-intent reporting rather than premature convergence.
- **Clarified worker boundaries:** an initial obstacle does not prove the intent exhausted; finish relevant approaches before concluding.
- **Removed per-asset related from insert_assets.** It only controlled automatic scope inclusion, was not persisted, and could not be inspected or retained across repeated registration.
- **Decoupled task_scope from the coverage switch.** insert_assets always accumulates automatic scope, and add_task_scope remains available to planner/main-agent/goals. Coverage controls denominator metrics, not scope accumulation. Previously disabling coverage disabled both auto and agent insertion paths, leaving manual scope only; list_untested_assets remains hidden without coverage. Historical notes described scope as authorization; current enforced network authorization is separately documented in the worker guide.

#### Fixed

- **Fixed cross-task asset contamination** (#59). list_assets formerly hard-coded task ID zero and queried the shared global asset store, so agents could treat another task's IPs as their own targets. Task runs now return assets owned within this task's/direct source tasks' task_scope: a root domain includes descendant hosts/services/endpoints, and a CIDR includes hosts/services. Direct ID lookup cannot bypass scope. Taskless Auto/pentest retains global lookup. Also fixed ownership matching for direct IP URLs. The producer-filtered UI asset list is unchanged.
- **Restored worker trace tools after an old pruning migration.** search_all_worker_traces, get_worker_trace and node_detail were removed from that migration's unbinding list, with a one-time repair for databases that already ran it.
- **Fixed intermittent /btw checkpoint writes** by removing JSONB-incompatible escaped NUL before persistence.

### Triggers

#### Fixed

- **Deduplicated task descriptions/goals in merged triggers**, preventing repeated same-task context from bloating multi-task notifications.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- Added multiple interactive main-agent conversations per task, with creation, switching and independent context reset.
- Added persistent /btw side questions without interrupting the main run; answers survive restart.
- Added source_task_ids to spawn_task for read-only inheritance of source assets/findings.

#### Changed

- Disabled worker cross-engagement memory and narrowed its default tools: planning owns context review; workers execute and record a single intent.
- Shortened worker prompts, removed terminated/worker_name from get_worker_output, and simplified insert_assets/list_assets descriptions.

#### Fixed

- **Continued thought-only idle turns.** Some models ended a turn after thinking without text/tools; the harness treated that as completed with an empty summary. Existing connection, empty-response, safe-stream, breaker and model_error intent retries could not catch it, because thinking events were real events and no failure occurred. The worker Stop hook now injects a continuation instruction using the existing thought rather than replaying the same stable prompt/context and thinking again.
- Reused the empty-response retry budget, default two, with -1 disabling this behavior. It is a total per-intent cap, not merely a consecutive cap: the harness permits one push per idle streak and refreshes eligibility only after a real tool turn. This bounds tool/idle/continuation cycles. Logs identify the worker, intent and n/N count.
- Fixed /btw context budgeting/layout in long chats and added insecure-context request-ID fallback.

### Traffic

#### Added

- Bound multiple traffic-evidence entries to a finding, with order, notes and request/response/proof roles.
- Report agents automatically link relevant evidence before report writing.
- Added an agent traffic-binding switch and completed inter-agent evidence handoff.

### Tasks

#### Added

- Added finding retests in independent agent conversations, with running state in list/detail views.

### Interception

#### Added

- Added approval details and execution audit showing request context, initial model/rule decision, output and argument fingerprints.

### Assets

#### Added

- Added DSL search for task assessment assets.

### LLM

#### Fixed

- Added the configured session header to connection probes, fixing opencode zen HTTP 400 responses.

### Network

#### Added

- Historically added optional TLS-verification bypass for HTTP MCP self-signed endpoints. Current isolated MCP policy requires TLS verification.

### UI

#### Added

- Grouped conversations by agent with collapse, independent pinning and agent filters.
- Rendered digest nodes and folded their members in the exploration graph.

#### Fixed

- Fixed blank screens caused by out-of-sync login credentials.
- Reworded interception notices as explicit platform policy so agents do not mistake them for target defenses.

### Deployment and updates

#### Added

- Added optional in-page updates with a settings card/top-bar indicator: download, verify SHA256SUMS, smoke test, stage, request supervisor restart, replace and refresh.
- Added start.sh/start.bat supervisors to release packages and images, forwarding SIGTERM and acting on exit codes while Go owns verification/replacement.
- Failed verification/smoke tests discard staging and retain the current version; three failed starts roll back. Manual rollback does not revert database schema.
- Updates require HTTPS GitHub domains and a fixed release source. Development builds disable updating; checks cache for thirty minutes.

#### Known limitations

- Container updates replaced only the application, not its image/toolchain; recreating the container restored the image version, requiring an image update separately.
- Bundled release skills were not synchronized, so newly added built-in skills did not appear automatically.
- Updating restarted the service and interrupted active tasks.

### Dependencies

#### Changed

- Updated norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- Added a retry/backoff settings tab for five nested layers: pre-stream connection reset/timeout/429/5xx retries; OpenAI empty-content responses; same-provider replay before any output; pool circuit breaking/cooldown; and whole-intent rerun after model_error. Inner budgets exhaust before outer ones. Each layer accepts attempts and interval: empty uses defaults/exponential backoff, explicit interval is fixed, and -1 disables attempts. The first three are per-endpoint overrides; breaker/intent are global. Changes apply hot, and all-zero/new absent settings preserve old defaults. Connection probes omit retry overrides because their hard thirty-second budget would otherwise misdiagnose healthy endpoints.
- Added per-profile session_header_key. Requests derive the header value from context (for example conv-<id> or exp<x>-worker-i<intent>), supporting gateway prompt caching/sticky routing without changing norma. Shared providers still use distinct session values; migration/UI fields were added.

#### Fixed

- Fixed profile saves with an empty session header: removed NULLIF on the NOT NULL DEFAULT empty-string column, which had incorrectly produced PostgreSQL 23502.

### Agent

#### Added

- Changed wall-clock timeout to in-session settlement with norma v0.3.6. MaxDuration interrupts the active tool and performs bounded writeback/summary on a live context, ending timeout instead of relying on worker/planner maxDur+90s hard cancellation as aborted_tools. Chat uses the same harness; the unlimited main-agent path is unaffected.
- Added a stalled-planning fallback: on heartbeat/unchanged wakeup with no open/running intents, explicitly report no workers/queued directions and require at least one new nonduplicated intent rather than zero.

#### Changed

- Moved full intent/start instruction/anchored-asset JSON into the rebuilt system prompt so compaction or transcript loss cannot erase it. The initial user message now contains only a degradable/stale-tolerant overview. This deliberately sacrifices cross-intent prompt-cache reuse to retain intent.
- Shortened planner/worker defaults. Planner reviews blocked/exhausted traces before choosing next work, treats negative observations as tentative evidence rather than final conclusions, must produce work when unmet goals have no open/running intents, and prioritizes depth. Workers record tentative negative observations and send cross-intent leads to the planner through fact summaries. Reseeding appends/selects a new default version while retaining user edits/history for rollback.
- Narrowed worker tools to single-intent execution/writeback, removing list_facts/node_detail/list_companies and cross-work trace tools, while keeping finding deduplication and asset/fact writes. Removed stale insert_assets fields conflicting with its schema. A one-time migration unbinds only worker roles, leaving planner/main bindings unchanged.

### Exploration graph

#### Added

- **Added reversible cold-node digests.** Old inactive intents/facts fold into graph_overview digest nodes while originals remain permanently retrievable by ID. Reverse reachability keeps any active branch hot, with six-round debounce and connected-component grouping. Background minor/major compaction rechecks activity and cooldown, avoids the hot path and never overwrites revived nodes. cold_digests/cold_index support expand_digest/expand_index; inherited tasks use their own folded views, with cross-task read-only expansion. Only planner/main-agent receive expansion tools; old databases migrate.
- graph_overview included complete finding_list records because findings are high-value and usually few. Compact entries contain id, summary, optional evidence/from_intent/assets, with readable URLs/domains/IP:port rather than bare asset IDs.

#### Changed

- Removed flattened host strings from graph_overview.coverage, retaining host_count and on-demand list_assets. Added done_intents_total beside the bounded recent_done_intents window so planner deduplication can detect truncation.

### Tools

#### Changed

- Moved add_company_scope default ownership from worker to planner/mainagent/auto, including a one-time old-database migration.
- Bound list_assets to planner alongside list_untested_assets; a one-time backfill retained intentional user unbindings.

### Tasks

#### Added

- Added a Running workers task-list column counting running intent nodes, matching task overview and showing zero when none run.

### Network

#### Added

- Added a global target egress proxy (HTTP/HTTPS/SOCKS5 with optional credentials), independent of model and web-search proxies. With capture enabled it was the MITM proxy upstream for intercepted and passthrough traffic; without capture it entered Bash/WebFetch configuration, including ALL_PROXY for SOCKS5. Stored in settings without a schema migration. Current isolated-worker network behavior is documented separately.

### UI

#### Fixed

- Corrected intent status labels: exhausted means budget exhausted, not fully explored; blocked means execution error after model/API/network retry exhaustion, not target/WAF interception. Added stopped for user-stopped work.

### Dependencies

#### Changed

- Updated norma to v0.3.4 for MCP output truncation/spill files (651b961), then v0.3.6 for wall-clock settlement.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- Added per-profile output-token limits and field selection. Zero omits the limit; context-window capacity remains a local compaction threshold. OpenAI Chat Completions defaults to max_tokens but can use max_completion_tokens for reasoning endpoints that reject the former. Anthropic and Responses keep their fixed max_tokens/max_output_tokens fields, so the selector is disabled/cleared for those formats. Wired the limit into planner/worker/chat/main-agent/goals, resolved per call and after failover; new zero/empty columns preserve old behavior.
- Connection probes logged each HTTP status and up to 4 KiB of gateway response for diagnosing authentication/quota/empty-frame/HTML errors independently of recording. These are historical notes, not a recommendation to log credentials or sensitive provider output.

#### Changed

- Allowed task LLM-chain edits in every state, including done/failed/timeout, because main-agent conversations still use the chain after task completion. Removed HTTP/transaction terminal-state gates and opened UI editing. Saving a terminal task no longer reopens quota-blocked intents without workers to consume them; explicit intent rerun or new goals resumes execution.

### Agent

#### Changed

- Reused pause/resume and transcript resume for messages steering a running worker instead of a separate persistence protocol affecting scheduling barriers and many queries. Input enters the next turn in a dedicated goroutine outside the ordinary three-slot pool. The UI keeps the input, restores direct continue, and uses SSE. No schema change; the explicit tradeoffs are in-memory-only steering without crash recovery and a temporary extra worker for this low-frequency action.

### Tasks

#### Fixed

- Streamed large-task archive snapshots instead of loading whole tasks into memory. Fixed cold-archive recovery gaps: SQLite-only traffic now always gets a staging journal, preventing a crash between PostgreSQL/SQLite commits from leaving archived traffic stranded in hot storage; missing journal.json is discardable.
- Reduced delays with many tasks by optimizing task/context/exploration queries and eliminating duplicate dashboard/chat/detail requests.

### Assets

#### Changed

- Removed the 256-rule company-scope limit from backend, frontend and demo. Individual rules remain capped at 1,024 characters and the request body at 2 MiB, bounding very large submissions without forcing ordinary per-host scopes into multiple companies.

### Skills

#### Fixed

- Added pure-Go ZIP bzip2/Zstandard decompression beyond standard Store/Deflate. Unsupported Deflate64/LZMA/XZ/PPMd and encrypted entries are identified before extraction with the file/method and repackaging guidance.
- Replaced ASCII-only file-path validation with Unicode-aware rejection of control/invalid UTF-8/zero-width/bidirectional characters, unsafe punctuation, traversal, absolute paths and empty segments. This allows international filenames/spaces without weakening traversal protection. Skill names retain lowercase ASCII letters/digits/hyphens while allowing non-ASCII letters, but not spaces/dots/separators.
- Added GBK filename fallback for Windows ZIPs without UTF-8 flags, followed by normal validation, and parsed quoted frontmatter names correctly.

### UI

#### Added

- Added default All findings flat view alongside grouped-by-task view, with 10/20/50/100-row pages, export selection, evidence/report expansion, inline metadata/status edits, deepen and delete. Views share filters/statistics, persist preferences, poll only the active view and update both caches on mutation.
- Added By asset: company/root/IP/subdomain/service/endpoint tree plus shared findings table filtered to a selected subtree. Include only assets with findings and needed ancestors; deduplicate subtree finding counts and group missing/deleted assets as unassigned. Refresh on entry/filter/mutation/manual action instead of polling navigation every five seconds. Relative labels preserve full hover/breadcrumb values; excessive endpoint/service levels can be omitted with a notice while counts roll up.

#### Fixed

- Collapsed the session list on narrow screens so it no longer consumes transcript space in mobile task details.

### Dependencies

#### Changed

- Updated norma v0.3.2 to v0.3.3 for Config.MaxTokensField, sending exactly one of max_tokens (default) or max_completion_tokens.
- Updated golang.org/x/mod v0.37.0 to v0.40.0 for two dependency alerts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed non-streaming settings appearing to revert (#69). The list DTO omitted streaming, so the UI defaulted undefined to true despite correct DB writes. The field now returns even when false.
- Connection probes require an actual model reply and use the configured streaming mode (#65), rather than accepting HTTP success with no content or always testing a different SSE path. Successful probes show the response; empty replies fail.

### Agent

#### Changed

- Paginated/filtered list_facts (#74): latest twenty by default, limit capped at one hundred, before cursor and summary keyword q, with facts/total/has_more/next_before. Long summaries truncate by characters; node_detail supplies full text. Updated worker/planner prompts and migrated tool schemas in existing catalogs whose insert-only seeding otherwise showed no parameters.

### Tools

#### Added

- Added lazily loaded command-call statistics (#72): count, proportion and failures by tool, descending counts across the entire task/query-filtered result, not just one page.

### Dependencies

#### Changed

- Updated norma v0.3.1 to v0.3.2 for previously dropped OpenAI reasoning/refusal (including Responses reasoning_text), deduplication across reasoning aliases, bounded empty-content retries, and compaction tool-pair/orphan repair for existing transcripts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Added real streaming/non-streaming per profile, default streaming. stream:false receives one complete JSON response to work around broken SSE gateways, at the cost of live progress/token updates. Worker/planner/main-agent/chat/goals resolve it dynamically; llmpool/llmrec/task-runtime wrappers support both. A migrated streaming column defaults true.
- Added openai-responses as a third format using POST /v1/responses, normalized BaseURL and default model gpt-5, alongside Chat Completions/Anthropic. Migrated format constraints and UI; norma v0.3.1 also restores reasoning_content.
- Recorded actual HTTP wire bodies at transport level, including schemas/tool_use blocks/raw SSE omitted by normalized views and each internal retry attempt. Migrated raw_request/raw_response columns and added original-view/copy controls with insecure-context clipboard fallback.

### Conversations

#### Changed

- Moved conversation selection into an explicit Select multiple mode. The default list has no permanent checkboxes; selection mode offers select-all/batch delete, Done clears it, all-success deletion exits it, and failures remain selected for retry. Single rename/pin/delete actions stay in each row menu.

### UI

#### Changed

- Improved task/conversation/traffic interaction (#57): persisted task-column ordering, direct rename icons, drawer behavior and traffic detail presentation.

#### Fixed

- Limited worker asset labels to domains/IPs and handled empty labels.

### Agent

#### Added

- Added quantitative goal acceptance checks before prove_goal: measured coverage, flag counts or permissions must satisfy the stated threshold in graph_overview. Unmet measurements require more work rather than approximate completion. This fixes a 100%-coverage goal being accepted at 40%.

#### Changed

- Reframed zero-intent planning: it is appropriate only when existing open/running/recently finished work covers every direction, or the next decision genuinely awaits running output. Uncovered independent directions and unmet goals with untested scope must not stop just because zero intents were called common.
- Strengthened negative-evidence standards for workers: reasonable in-intent alternatives must be exhausted before observed claims such as no injection, closed port or no login entry. Incomplete/weak evidence uses inferred confidence rather than closing off the planner's route prematurely.
- One-time guarded planner/worker prompt migrations append/select a new version and retain prior/custom versions for rollback.

### Operations

#### Added

- Added reset-password.sh for the fixed ARTEX administrator using local/Compose/container PostgreSQL. Connection options can come from explicit arguments, DSN/environment or config.json. pgcrypto generates a backend-compatible bcrypt hash under auth.password_hash without service restart. Password delivery to psql uses escaped environment-backed values; explicit CLI options still require care with process visibility.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- Added transactional batch category moves, including uncategorized; failed items can only be tasks deleted after selection.

#### Changed

- Replaced the task-category dropdown with searchable creation/selection: Enter or Create adds an unmatched name and selects one removable chip.

### Conversations

#### Fixed

- Unified pre-send and execution model resolution so a selected conversation profile works even without an active global profile. Conversation/agent binding now precedes global fallback in both places.
- Distinguished no configuration from configured-but-inactive in actionable chat notices.

### Agent

#### Added

- Allowed main-agent add_intent work after all goals are met: workers execute it directly while planner stays idle, avoiding a repeated goal-completion decision canceling the new work. The task returns done when the frontier drains. Main-agent asks whether the user wants the work registered as a formal goal; doing so restores ordinary autonomous planning.

#### Changed

- get_worker_trace/get_task_worker_trace now return the first five valid unique requested steps rather than erroring on a larger list. returned_step_ids, omitted_step_ids and notice identify remaining steps and discourage unnecessary extra reads.

#### Fixed

- Sorted tasks by descending ID instead of timestamp, avoiding equal-time tasks swapping places during polling and random map iteration.

### Assets

#### Fixed

- Used try_inet() instead of failing casts when legacy assets.ip contains a hostname, preventing one dirty row from rolling back company-scope edits/deletion.
- Reported invalid IP counts, IDs and values in save warnings and logs so operators know those assets do not match IP/CIDR ownership, including non-UI sync/agent paths.
- Used try_inet() for task IP/CIDR matching too; the old character regex admitted hex-letter hostnames such as abc.def and still failed.

#### Changed

- Rejected hostnames in ip fields for ip/service/endpoint assets. insert_assets and asset APIs return per-index guidance to use a subdomain/domain or resolve A/AAAA first, while valid batch entries still insert.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- Added individual worker pause/resume/cancel. Pause retains intent/facts/findings; cancel transactionally removes the intent/direct outputs after execution stops.
- Added paused state, execution barriers and named terminal causes to prevent late writes after cancellation/deletion.
- Added optional task concurrency with persisted FIFO admission for creation/resume/deepen/slot refill.

#### Changed

- Increased default worker wall-clock budget from 600 to 1,200 seconds; revived timeout tasks restart timing at their next real execution.
- Drove task badges from actual worker/planner/main-agent execution, fixing idle while a worker ran.
- Kept per-session controls/token totals and moved model names beside the selected session title.

#### Removed

- Removed worker multiselect/select-all/batch pause-resume UI, API and mocks.
- Removed row token badges/tooltips while retaining the complete usage ledger and task aggregates.

### LLM

#### Added

- Added ordered task LLM chains with explicit quota-based failover and persisted current/exhausted/error state.
- Running/paused tasks can edit/reorder chains or manually select the next-call profile.
- Automatic/manual switches and exhaustion create structured, deduplicated activity notices.
- Added consistent next-call role model-resolution endpoints.
- Added optional global LLM Pool ordering, bound-model fallback, health, cooldown and reset.
- Added an always-on usage ledger grouped by task, session, model and profile with input/output/cache tokens.

#### Changed

- Goals/planner/worker/main-agent share task-level model runtime.
- Task-chain failover reacts only to explicit quota/balance/billing errors, not rate limits/auth/network/server/context errors.
- Reorganized model settings into cards/drawers plus pool order/priority/exclusion/health/reset controls.
- Wrapped exhaustion text on narrow screens and showed the selected model beside the session title with a tooltip.
- Role resolution order is agent binding, task chain, then global; explicit bindings remain exclusive.
- Empty outbound proxy means direct, without HTTP_PROXY/HTTPS_PROXY fallback; explicit ARTEX_LLM_PROXY still applies and authenticated SOCKS5 URLs are accepted.

#### Fixed

- Retried transient stream failures on the same provider only before any output was delivered, with backoff. Quota/context/deterministic 4xx errors remain with failover or compaction recovery instead of replay.

#### Removed

- Removed LLM icons from the conversation list to avoid confusion with worker status icons.

### UI

#### Added

- Added global task category create/rename/delete/filter and category selection at creation.
- Added task-template CRUD/drawer, preset description/goals and Save as template.
- Added multiple source-task/company scopes, with multiline recognition of domain/URL/IP/CIDR/ICP/company keywords.
- Added live task-asset changes and manual/company/inherited/discovered provenance labels beside worker sessions.
- Grouped findings by task with independent pagination and high-priority deepen intents.
- Added conversation rename/pin/unpin/delete and current-page task pause/resume.
- Moved traffic/command details to drawers, completing Host and syntax-highlighted request/status/header/JSON/markup views.
- Added task detail/pause-resume actions, configurable send keys and a web-search proxy field.
- Showed profile names beside conversations with model-ID tooltips; optional task names appear in search/lists, falling back to descriptions.
- Added skill call counts, recent use and missing-dependency statistics.

#### Changed

- Made task titles focusable detail links. Task/company assets and grouped findings use real server pagination, stable ordering and exact totals.
- Moved company creation to a drawer with multiline scope recognition, validation and type previews.
- Added growing multiline main-agent input, Enter send/Shift+Enter newline and IME composition protection.
- Shared Markdown rendering across previews/reports/details; fixed long confirmations/errors and mobile drawer widths.
- Template selection displays/searches names but submits IDs; restored original font sizing and displayed application version 0.3.3.
- Simplified token totals to highlight input/cache-read/output and used an upward-arrow send button.
- Showed company names rather than IDs in overview scope.

#### Fixed

- Stopped treating ordinary dotted text as an ICP identifier.
- Deduplicated variable names colliding with global runtime variables such as {{.Now}} in the agent editor.

#### Removed

- Removed the task card's separate Enter button in favor of its title link.
- Removed the dashboard create-task button, company Logo URL input and finding-count badges in finding/task headers.
- Reverted the global 10% font increase and removed worker selectors/model icons/row token counts from conversation lists.

### Agent

#### Added

- Added read-only inheritance from multiple direct source tasks: goals/facts/findings/completed intents/scope/blackboard context.
- Blackboard tools can query inherited nodes/facts/findings/traces on demand; provenance is marked and write tools reject inherited modification.
- Company scope recognizes domain/URL/IP/CIDR/ICP/keywords; keywords guide agents but do not automatically assign asset ownership.
- Deepen creates a high-priority human intent in the original task with anchored assets and derived_from edges.
- Added goal-management overview CRUD. Add/edit notifies planner and revives the task; hard deletion cascades edges/anchors but does not revive.
- Added steer_work to main-agent for live same-task worker correction without losing progress.

#### Changed

- Source intents stay out of a new task's frontier; exploration, queue, workspace and conversations remain independent.
- Deleting active intents historically stopped them with a required reason recorded as a fact/payload and planner cancelled notification, retaining their content.
- Main-agent chat uses server activity streams, restores failed-send input, and keeps the last lazily expanded answer visible.
- Pausing cancels current main-agent/planner/worker calls but permits a new user-initiated main-agent conversation while paused.
- Cancellation/shutdown/stream interruption retains real causes, generated content, turns, duration, tokens and unmatched calls.

#### Removed

- Removed optimistic main-agent message echo to prevent duplicates or cross-session content during pause, failure and concurrent activity.

### Constraints

#### Added

- Goals decomposition extracts allow/deny constraints first; main-agent can supplement them. Planner/worker prompts prioritize constraints over diversification/exploration.
- Added constraint-management CRUD and independently enabled planner/worker injection, on by default and reread each turn. task_constraints is created idempotently for old databases.

### Interception

#### Added

- Added model fallback review after no string/regex rule matches, returning ALLOW/ASK/DENY with configurable model-failure and approval-timeout actions. Split the UI into rules/model tabs; model reasons retain the historical model-prefix sentinel.

#### Fixed

- Hardened verdict parsing so a correct DENY is not misclassified as allow.

### Traffic

#### Changed

- Stored whole traffic exchanges in SQLite with hash-deduplicated large-body blobs and trigram substring/Unicode search. Deletion became one SQL transaction, reducing hours to milliseconds without pausing recording. Added streamed large-body downloads and traffic_search full-text parameters; old filesystem traffic remains readable/searchable/deletable without migration.

### Tasks and assets

#### Added

- Added the default-on coverage switch. Historically off disabled coverage/display, automatic task_scope accumulation and related planner/main-agent tools while preserving company associations.
- Added per-asset related=true for insert_assets; with coverage on, false inserted only into the shared store without task coverage.
- Unified company/task scope text recognition for domains, URLs, IPs, CIDRs, ICP and keywords. Domain/IP entries create or reuse assets; other entries remain scope context.

### Build

- build.sh --release builds Linux/macOS amd64/arm64 and Windows amd64 ZIPs containing skills, example config and README.
- Linker stripping plus ZIP are default; UPX is explicit to avoid self-extracting ELF startup faults on some Linux systems.
- Release CI smoke-tests the Linux amd64 binary and supplies SHA256SUMS.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- Added multiple source-task inheritance of facts/findings/completed intents/scopes/blackboard, retaining independent queue/workspace/history.
- Added ordered task LLM chains with explicit quota failover and persisted selection/exhaustion/audit state.
- Running/paused tasks can edit/reorder/switch chains, with automatic/manual/exhaustion notices.
- Added worker pause/resume/cancel; cancellation cleans the intent/direct facts/findings/execution records only after writes stop.
- Task deletion can also remove assets/traffic/findings/files, with barriers, concurrency protection and auditable counts.
- Added FIFO task concurrency admission and automatic slot refill.
- Added task/company asset pagination, category statistics and paginated intent mocks.
- Added build.sh for static frontend export, embedding, cross-platform targets and version injection.

### Changed

- Explicit task chains coexist with the global pool, keeping strict quota-only chain failover and ordinary agent/global rules when absent.
- Main-agent input grows, uses Enter/Shift+Enter and respects IME composition.
- Task roles share runtime and use the smallest candidate context window for safe compaction thresholds.
- Task detail state follows real calls; inherited facts/findings/intents/assets/nodes are provenance-marked and read-only.
- Shared Markdown rendering across agent/report/detail views.
- Model settings use cards/drawers with independently scrolling model lists.
- Paused tasks allow new main-agent orchestration messages; pause cancels only the current turn.
- Increased worker default wall-clock budget from 600 to 1,200 seconds.

### Fixed

- Replaced optimistic main-agent echo with server-driven messages, fixing pause/send-failure/cross-session confusion.
- Open conversations scroll to the complete latest reply after lazy detail expansion.
- Fixed main-agent runs continuing after pause and orchestration pause failing to stop the main agent.
- Synchronized task badges/actions with completion and actual role execution.
- Prevented late writes/residual files during worker cancel, task deletion and concurrent writes.
- Fixed Markdown preview, overflowing confirmations, mobile widths and detail-button alignment.
- Added named causes to all cancellation paths, displaying initiator, terminal state, turns, duration, tokens and unmatched calls.
- Fixed parent shutdown cancellation overwriting the named shutdown cause, and byte truncation corrupting Unicode activity summaries.
- Retained the real termination cause and partial generated output for interrupted streams.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

[Unreleased]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...HEAD
[0.3.10]: https://github.com/Autumn-27/ARTEX/compare/v0.3.9...v0.3.10
[0.3.9]: https://github.com/Autumn-27/ARTEX/compare/v0.3.8...v0.3.9
[0.3.8]: https://github.com/Autumn-27/ARTEX/compare/v0.3.7...v0.3.8
[0.3.7]: https://github.com/Autumn-27/ARTEX/compare/v0.3.6...v0.3.7
[0.3.6]: https://github.com/Autumn-27/ARTEX/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/Autumn-27/ARTEX/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Autumn-27/ARTEX/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Autumn-27/ARTEX/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Autumn-27/ARTEX/compare/v0.3.1...v0.3.2
# `/btw` validation record

Historical record: 10 September 2026, branch `codex/btw-side-question`, baseline
`8dae851b9b622f2ff2631f332fde9719d0b16fba`. This English translation describes checks
reported by the supplied repository; it does not claim they were rerun during the
current security review.

The original run used an independent PostgreSQL database/data directory. Live
credentials were injected only into its isolated test environment and were not
written into code or this record. Product model defaults were not changed. Its
versions were Go 1.26.3, Norma 0.3.6 and Next.js 16.2.9.

Live conversations, returned objects, engineering assertions and the original Qwen
review are recorded in [validation-2026-09-10.json](validation-2026-09-10.json).
The supplied record states that it contains no API credentials.

## Engineering checks

The historical record reports these passing checks:

- Deep-copy structured messages/tool arguments; summary/compaction calls do not
  overwrite snapshots; publish complete/terminal boundaries and exclude partial
  generations: `TestCheckpointDeepCopyAndBoundaries`,
  `TestSnapshotExcludesPartialStreamAndSelectsPoolMember`.
- Actual selected provider-pool identity:
  `TestSnapshotExcludesPartialStreamAndSelectsPoolMember`.
- Tool pairing, 20-pair replay, budget reduction and overflow:
  `TestBuildRequestCompactionToolPairingAndBudget`.
- Main/side concurrency and independent cancellation in both directions:
  blocking-provider `TestMainSideConcurrencyAndIndependentCancellation`.
- No tool execution, streaming/nonstreaming and reported usage before failure:
  `TestServiceNoToolsAndUsageOnFailure`.
- Actual Norma ChatAgent/local Read tool with main transcript/activity isolation:
  streaming/nonstreaming `TestSideActualChatCheckpointToolResultAndTranscriptIsolation`.
- Persistence, pagination, idempotency and partial recovery after restart:
  `TestSideHistoryIdempotencyPagingAndRecovery`.
- Clear/late-write races, parent deletion and version comparisons:
  `TestSideClearLateWritersAndDeletedParent`.
- MainAgent/Worker archive restoration across v1/v2/v3: `TestSideTaskArchiveVersions`.
- Three parent interfaces, authentication, resource ownership and logical Worker
  deletion: `TestSideHTTPGlobalLimitTaskWorkerAndDeletion`,
  `TestSideCheckpointPersistsBeforeAdmissionAndRestart`.
- Side questions during busy main execution, independent SSE reconnect/disconnect,
  cancellation and clear: `TestSideHTTPBusyIsolationClearAndReconnect`.
- One request per parent and four globally: the two `TestSideHTTP...` cases.
- Save snapshot before submission, ask again after restart and reject fabricated
  old-session snapshots: `TestSideCheckpointPersistsBeforeAdmissionAndRestart`.
- Reject deleted/changed cached profiles: `TestSideRejectsDeletedOrChangedCachedProfile`.
- Drain/cancel and persist final answer/usage before archive:
  `TestSideTaskDrainPersistsBeforeArchive`.
- Early streaming-consumer cancellation records usage exactly once under the side
  question: `TestSideUsageRecordedOnceOnConsumerCancellation`.
- Restored Workers/deadline contexts continue publishing checkpoints:
  `TestSideRestoredWorkerRuntimePublishesNewCheckpoint`.
- Related-package race checks, TypeScript/production builds and Biome checks for
  three newly added frontend modules.

Reproduction requires a disposable test database, never a production DSN:

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

**The historical full Go regression suite was not entirely green.** Two existing
server tests failed temporary-directory cleanup with `TempDir RemoveAll ...
directory not empty`: `TestInheritedActivityDetailAndRelationDeletion` and
`TestTaskMetadataPatchReturnsRenameAndPin`.

The unchanged baseline reproduced both cleanup failures in the same isolated
environment. Its run also separately failed a target-node count assertion in
`TestCoreTaskLifecyclePG`, which did not fail in the final historical server run.
Other packages, side-question cases and race checks passed. Baseline failures were
not counted as acceptance passes and existing assertions were not weakened to hide
them. Next.js reported existing multiple-lockfile/workspace-root warnings but
completed its build and generated all pages.

## Browser checks

The historical Codex in-app browser connected to independent local Go/Next servers
at desktop and 390×844 mobile sizes. Its record reports:

- `/btw` during regular chat displayed main output and side questions concurrently.
- Follow-up requests worked; stopping a side question retained partial output and
  left main execution running.
- Closing/reopening the panel and refreshing recovered running/completed history.
- Mobile drawer input, buttons, history and close controls worked without
  horizontal overflow.
- Clear required confirmation, removed history and retained main transcript/snapshot.
- MainAgent and two Worker tabs retained separate histories.
- A blocking local-model fixture kept a Worker active while a side question was
  submitted/canceled. The Worker's live state/pause button remained intact, and
  partial side output was saved.
- Browser error/warning logs were empty.

The fixtures controlled concurrency timing rather than relying on live model
speed. Two preliminary Worker checks lacked a valid concurrent interval because
the task or answer had already completed; they were not counted as passes. The
corrected fixture passed.

## Historical live model conversations

A probe of `grok-4.6` at `http://127.0.0.1:12580/tingly/openai` returned HTTP 200,
model `grok-4.6` and `READY` in 2.82 seconds. Because the primary worked, Tingly
`glm` and Zhipu `glm-5.3` fallbacks were not activated or validated.

- While the main session ran, a question about assets/goals/markers returned
  `redhaze.top`, the first-page/goal summary and `BTW-REAL-0910`; completed in 16.97 s.
- A later evidence question correctly cited WebFetch 200, curl redirects
  301 → 302 → 200 and the page title; completed in 7.24 s.
- A request for the side question to create a file through Bash was refused; the
  file was absent; completed in 7.74 s.
- Completed side questions left main transcript SHA-256/activity unchanged and
  executed zero tools.
- A real service stop/restart retained three prior side questions and answered
  from the persisted snapshot without rerunning the main agent.
- Nonstreaming Grok in a new session returned the correct asset and `ATOMIC-0910`;
  saved usage was input 11,734, output 138, cache_read 11,520.

The main asset-check session read a public first page using WebFetch and Bash/curl.
Its landing URL was `https://id.redhaze.top/home`. The original returned title was
"红幕科技 RedHaze Group · 全球综合集团门户"; this is preserved as raw evidence rather
than translated. Curl briefly saved a local response file but did not write to the
remote system. That distinction is separate from the side question executing no
tools.

Main transcript SHA-256:
`e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

**Usage limitation:** Tingly Grok streaming returned no usage. A direct
`stream_options.include_usage=true` test returned HTTP 200, 12 data frames and zero
usage frames. Recorded zero therefore means usage was unavailable, not that the
request was free. Nonstreaming usage and fixture failure/cancellation usage were
saved correctly.

## Historical Qwen review

`qwen-flash` at `https://dashscope.aliyuncs.com/compatible-mode/v1` returned HTTP 200.
It received the first three live conversations, main tool evidence and engineering
assertions, then returned `verdict: accept` and `concerns: []`. It judged the
asset/marker/evidence answers consistent and the side-question tool refusal
appropriate. Usage: prompt 6,625, completion 312, total 6,937.

That review did not include the later restart/nonstreaming tests. Its broad
"no writes" wording was excessive: the main curl session did create the temporary
local file described above. Concurrency, zero side-tool execution and transcript
isolation rely on engineering assertions; model review is only supplementary
answer-quality evidence.

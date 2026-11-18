# Side-question history and context budgets

Historical design revision: 11 September 2026. This is an English translation of
the supplied implementation record, not a claim that its historical checks were
rerun during the present security review.

The previous implementation treated request-JSON character count as token count
and inherited the main task's 32K output reservation, prematurely rejecting valid
questions containing substantial HTML, JavaScript or tool results.

## Open-source implementation comparisons

- [Grok CLI context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739)
  extracts recent user/assistant text with roughly a 2,000-character budget and a
  400-character per-item cap; that path does not retain a side-question dialogue.
- [Grok independent requests](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts)
  have independent cancellation, a 2,048-token output limit when supported and no
  tools.
- [Grok compaction](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts)
  estimates tokens, retains recent content, updates previous summaries and handles
  truncation across turn boundaries.
- [OpenCode compaction](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts)
  estimates the full request, reserves output/buffer space, combines recent content
  with rolling summaries and uses tool-free summary calls. Its referenced defaults
  retain 8K recent tokens and permit 4,096 summary-output tokens.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts)
  retries only before assistant output begins and prevents recursive recovery.

ARTEX adopted separate output budgets, recent context plus rolling summaries and
bounded recovery. It preserved Norma 0.3.6 structured messages and tool pairing,
without copying Grok's plain-text extraction or inserting OpenCode-style main
compaction events into the ARTEX transcript. Current module versions may differ.

## Request budgeting and execution

- Estimate UTF-8 bytes per content block using Norma's approach, with a 4/3 margin;
  add system prompt, tool schemas and message-wrapper overhead. This is an estimate,
  not an exact tokenizer result.
- Side-question output defaults to at most 8,192 tokens and never exceeds the
  configured main output cap. `ARTEX_BTW_MAX_OUTPUT_TOKENS` permits 256–32,768.
  Product model selection and main-task generation parameters remain unchanged.
- Input budget equals context window minus output cap and safety margin. Unknown
  windows use the platform's 200K default. Safety margin is 5%, at least 128 and at
  most 8,192 tokens.
- Load successful pairs by increasing ordinal, at most 20 at a time. Keep at most
  20 raw pairs, using no more than one quarter of the input budget or 16K tokens.
- Older pairs enter a rolling summary carrying provenance and snapshot timing.
  Historical assistant answers are not new tool evidence; the latest main snapshot
  wins if they conflict.
- If main context remains oversized, summarize only older messages in a copy,
  retaining up to 8K recent tokens. Never split a tool-call/result group. Summarize
  an entire oversized group instead.
- Split summary input safely at UTF-8 boundaries to fit the remaining window;
  output is capped at 2,048 tokens. Empty, truncated, oversized or tool-calling
  summaries are not cached. At most 12 summary calls share the request's 120-second
  deadline. Exhaustion fails explicitly rather than looping indefinitely.
- On the first context overflow, retry once with less context only if no text/tool
  call has been emitted. Stop immediately if estimated size did not shrink.
  Other model errors and partial output do not trigger recovery.
- Accumulate reported usage from summaries, failed attempts and cancellations on
  the same request. Missing provider usage is recorded as zero, never fabricated
  from estimates.

## Persistence and interface

`side_question_sessions.memory` stores the older-dialogue summary, summarized
ordinal and main-context summaries keyed by snapshot identity.
`side_question_requests.context_info` records preparation stage, actual replayed
pair count, summary use and budget estimates.

Save summaries only while the original request is running and its clear epoch
matches. Clear removes caches; late writes cannot resurrect cleared state. New
snapshots do not reuse old snapshot summaries. Archive v3 retains summary fields;
older v3 records missing them restore empty objects, and v1/v2 remain compatible.

POST admits the request before background preparation/compaction and holds neither
an admission lock nor a database transaction during inference. SSE/history show
preparation, history summarization, copy compaction and answer stages. Compression
errors become terminal request failures. The UI retains errors and restores the
failed question draft without covering the input with a floating error toast.
History polling no longer clears submission errors.

## Historical validation record

The supplied record reports these results:

- Replay at 19, 20, 21 and 50 pairs; preservation of older conclusions; restart
  reuse of summary caches: passed.
- Long Unicode answers/code, budget splitting, tool pairing, snapshot immutability
  and cache invalidation on new snapshots: passed.
- Summary failure/cancellation/truncation/oversize/tool responses, clear races,
  call caps, single overflow recovery and no retry after partial streaming: passed.
- Isolated PostgreSQL pagination/restart, v1/v2/v3 archives, summary/budget metadata
  restoration, older v3 records and 20 parents sharing four global slots: passed.
- Candidate Go build, TypeScript, changed-component Biome checks and independent
  Next production build: passed.
- Independent browser fixtures at 1280×720 and 390×844 checked preparation stages,
  summary scope, failed-draft recovery, absence of floating errors, horizontal
  overflow and console errors. Temporary fixtures were removed.
- Read-only replay of an existing Worker snapshot passed the new budget check;
  Worker #3's 293,085-character snapshot was no longer falsely rejected by a local
  character-count check. No external model was called for that replay.
- Automatic approval review rejected sending a private Worker snapshot to Grok;
  that live conversation was not run and was not counted as passed.
- At the user's request, the historical local backend was restarted at 00:37 on
  11 September 2026 using its existing database/data/login settings. Candidate and
  executable SHA-256 values matched; direct/proxied health endpoints succeeded.

Historical commands used an isolated test database:

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/artex
npx tsc --noEmit
npm run build -- --webpack
```

The historical frontend build used a separate copy to avoid overwriting an active
preview's `.next`. The candidate was `/private/tmp/artex-btw-budget-candidate`,
copied to `/private/tmp/artex-btw-preview/artex`; its prior binary was backed up
as `artex.before-context-budget`. These are historical paths, not current run
instructions or promises that those files still exist.

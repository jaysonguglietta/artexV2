package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nYour planning todos (retained across wakes from the previous round):\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Advance only steps whose prerequisites are complete and required facts exist. Update TodoWrite, marking steps satisfied by facts completed. Do not duplicate pending/in_progress steps.")
	return b.String()
}

// TriggerEvent identifies the concrete planning-round change: done fetches an intent conclusion; finding supplies its summary; goal carries all goals added by one call; goal_deleted/goal_edited carry old/new text; cancelled carries the deletion reason, with the stopped intent retaining it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // For cancelled events, retain the summary captured before actual node deletion.
	Goals    []string // For goal events, all goal texts added by this set_goals call.
	OldGoal  string   // For goal_edited events, the previous goal text.
	NewGoal  string   // For goal_edited events, the replacement goal text.
	Hints    []string // For hint events, all hint texts added by this add_hint call.
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nChanges that triggered this round (review these before adding directions):")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- The user (main agent) added a goal: %s. Add exploration directions if no corresponding intent exists.", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The user (main agent) added %d goals: %s. Add directions for each goal not already covered by an intent.", len(ev.Goals), strings.Join(ev.Goals, "；")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- The user (main agent) added a strategic hint: %s. It is attached to the graph; adjust or add directions where not already covered.", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The user (main agent) added %d strategic hints: %s. All are attached to the graph; review each when adjusting or adding directions.", len(ev.Hints), strings.Join(ev.Hints, "；")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- The user deleted this goal: %s. Reevaluate remaining goals and directions; do not dispatch intents for the removed goal.", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- The user changed the goal from [%s] to [%s]. Adjust directions to the new goal and stop dispatching obsolete directions.", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) reported a finding: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer the summary captured during deletion because the node may no longer be queryable.
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- The user deleted intent #%d, summary: %s, reason: %s. It will no longer execute; replan accordingly.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) finished, conclusion: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; new fact IDs yielded by this intent: %s ", fids))
			}
		}
	}
	b.WriteString("\n(Use node_detail / get_worker_output / list_findings for full details.)")
	return b.String()
}

// factIDsYielded lists the fact ids an intent produced this run as "#12、#15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, "、")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(Failed to retrieve output)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(This worker has no output record yet)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " ... (truncated; see get_worker_output for the full output)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\nCurrent situation (prefetched graph_overview; fetch node_detail/list_facts only as needed):\n" + string(b)
}

// plannerDefaultTmpl is the editable prompt body seeded into agent_prompts. The Goal template variable is rendered before code-owned artifactSpec is appended.
const plannerDefaultTmpl = `You are the planner for an authorized penetration-testing platform. You wake whenever the exploration graph changes. Read the current situation, evaluate goals, and add exploration intents only when a direction is not already covered. You plan; workers execute. Your outputs in this round must describe intents or evaluate goals, never carry out the investigation yourself.

Task goal: {{.Goal}}

How many intents should this round produce?
- Mandatory progress rule: if a goal remains unmet and there are no open or running intents (frontier_open=0 and running_intents is empty), produce at least one intent that advances the goal. No worker or queued direction exists to wait for. Reopen or continue a direction according to the done/exhausted/blocked rules below when necessary.
- Otherwise, zero intents is legitimate only with a concrete reason: every useful direction is already covered by open/running intents, or the next step depends on a running worker's output that does not yet exist. Rewording an existing intent is a serious duplication error. Do not dispatch downstream work before its prerequisite exists.
- Dispatch genuinely uncovered directions that do not depend on running work. Do not use zero intents as a default when in-scope surfaces remain untested.

Decision process on every wake:

1. The complete situation is appended below, equivalent to graph_overview; do not fetch it again without a reason. It includes the original task title and goal/root node, asset counts, goal states, open/running/recent_done intents, sites_without_endpoints, the count of exploration facts (distinct from vulnerabilities), and recent_facts with id, summary, and optional confidence.
   - Exploration nodes (goals, intents, facts, findings) belong to this task. The asset graph is shared globally; counts include globally in-scope assets, not exclusively this task's assets. Ignore unrelated assets.
   - Each intent includes parents (upstream facts/intents) and yields (downstream facts/findings); recent facts include from_intent. Use this lineage to combine evidence into new directions.
   - Negative or uncertain observations such as closed ports or failed injection attempts are worker observations, not final conclusions. Inspect node_detail(id) evidence before relying on them. Treat a direction as provisionally closed only with solid evidence, confidence=observed, and exhausted methods. Missing evidence, a single tentative probe, or confidence=inferred means unresolved. If in scope and uncovered, dispatch one verification intent to confirm or refute it. Recheck each negative direction at most once; respect a second negative conclusion with reasonable evidence.
   - Fetch details only as needed: list_facts supports q, before, total/has_more, newest-first pagination (default 20); list_findings returns vulnerabilities; node_detail(id) returns full evidence; list_assets supports q, type/company_id/task_id, pagination, or id/ids; asset_neighbors follows relationships. Do not fetch the entire shared asset graph by default.

2. Evaluate goals. For each unmet goal proven by a fact or finding, call prove_goal(goal_id, evidence_id, reason). Proving the last unmet goal automatically completes the task; there is no separate completion shortcut.
   - Never approve quantitative acceptance criteria early. For coverage percentages, flag counts, or required privileges, compare the measured graph_overview values (including coverage.pct and findings_total) with the exact requirement before prove_goal. If below target, dispatch work to close the gap. For example, 40% coverage does not satisfy a 100% requirement even if a major vulnerability has been found.

3. Optional initial orientation only: when the graph contains almost no facts and the task has just started, a few read-only Bash probes may clarify the first intent, such as one or two homepage/fingerprint requests. The only permitted output is a more precise intent description, never a vulnerability discovery, validation, exploitation, or enumeration result; workers do that work.
   - Once workers have produced facts (facts>0 or recent_facts is nonempty), do not probe yourself. Base decisions on existing facts and dispatch a worker for further investigation.
   - Even initially, stop after at most three probes. Endpoint/directory enumeration, iterating IDs, decoding chains, repeated requests to the same interface, and testing injection or access-control weaknesses are worker tasks. Stop immediately and describe an intent if you begin investigating rather than orienting.
   - Do not probe at all if existing facts already establish the direction.

4. Select new directions. Restraint means avoiding duplicates, not minimizing useful work. While goals remain unmet, consider deeper uncovered approaches rather than assuming it is time to finish. Intents are open exploration directions, not a fixed menu. Compare each with open, running, and recent_done intents:
   - Already covered by open/running: do not create another.
   - Previously in recent_done: inspect its state and actual evidence first.
     * done: do not dispatch the same work again. The yielded facts, not the state alone, determine whether it was a dead end. Retry only for materially new facts, assets, parameters, or a substantially different mechanism, explaining the difference in summary. Rewording or hoping another attempt succeeds is insufficient.
     * exhausted (budget ended mid-investigation) or blocked (model/network failure): inspect get_worker_trace / get_worker_output to learn what actually happened. Continue the previous progress if close to a breakthrough; retry after a purely external failure; change approach when repeatedly stuck at the same point. Base this decision on the trace, not just the state label.
   - Entirely uncovered: create an intent.
   - If every known direction is covered by open/running work, end this round and wait. If only recent_done remains and goals are unmet, the mandatory progress rule requires a new or continued direction.
   - Prefer depth over broad shallow coverage. Coverage is an acceptance floor, not the exploration goal itself. Prioritize following high-value entry points toward demonstrated impact rather than equalizing superficial asset coverage.
   - Maintain different mechanisms while goals are unmet. If all intents use one entry point but a substantially different uncovered route exists, add that route before duplicating the current one. Aim for two or three distinct approaches until evidence justifies concentrating resources. Operational constraints always take precedence: never generate an intent for excluded hosts, ports, surfaces, or operations.

   Sequential chains must be dispatched step by step. If step 2 needs step 1's actual result, do not run them in parallel. Record the whole chain with TodoWrite, one step per todo. Dispatch only steps with satisfied prerequisites, then advance on the next wake after the required fact exists and mark satisfied steps completed. Do not split the same action into separate confirmation and execution intents. Parallel intents are appropriate only for independent dimensions.

5. Submit selected directions in one add_intent call using its intents array: at most four highest-value intents, not repeated individual calls.
   - summary: one natural-language sentence containing the complete target address, the action, and the reason; no forced category template. This is the primary deduplication signal.
   - asset_ids: include the target asset IDs from list_assets whenever a concrete site, endpoint, parameter, or host is involved; include all relevant assets for a cross-asset direction. These support coverage, deduplication, and asset lineage. Leave empty only for genuinely global reconnaissance without a concrete asset.
   - parent_ids: optionally include every upstream fact, intent, or finding used to derive the direction. Leave empty for an independent top-level direction.

Avoid duplicates and artificial work. Dispatch useful uncovered directions while goals remain unmet. Be concise, focused, and efficient.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir) + langDirective(resolveOutputLang())
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Combine domain and SDK tools, omitting coverage-only capabilities when that feature is disabled. Production SDK tools are remote.
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// Put the completed-intent/global-graph situation in this round's user message, keeping system content stable for caching. Short planner rounds tolerate compaction of this lower-priority situation.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// In deadline terminal mode, add task-expiry settlement instructions to this round's user input so the planner evaluates final objectives without issuing new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\nTask final settlement (overrides normal planning instructions for this round):" + resolveTaskTimeoutWrapup("planner")
	}
	// Precreate the task workspace.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // Prompt constraints guide exploration; the worker network policy enforces destination authorization.
	}
	system, boundary := deferredSystem(sysBody, def)
	// The planner has no independent wall-clock budget; a task deadline clamps the current round to its remaining duration and selects task/per-run settlement wording by terminal reason.
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Legacy proxy options remain for compatibility fixtures. Production SDK target tools run in isolated workers and cannot reach the API recording proxy.
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Optional search uses the selected ddgs, Brave, Tavily or DeepSeek backend. Its dedicated provider proxy is independent of target traffic; empty selects direct transport.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Legacy proxy options remain for compatibility fixtures. Production SDK target tools run in isolated workers and cannot reach the API recording proxy.
		WorkingDir:            taskDir,                              // Task workspace under tasks/<taskID>.
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // Zero is unlimited; otherwise use the remaining task deadline.
		Compaction:            compactionConfig(p.compactionWindow()),
		// Planning todos persist across separate wake sessions so a serial plan survives subsequent rounds.
		Todos: p.todoFor(ts.ID()),
		// On this round's turn limit, persist planned intents, provable objectives and todos, rather than treating it as permanent planner shutdown. Deadline clamping selects task-aware settlement wording.
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // Use Provider.Complete for profiles selecting nonstreaming mode.
		MaxTokens:    p.maxTokens(),    // Zero omits the output cap and leaves the endpoint default.
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental noa compaction stores durable service-side archives by session when enabled.
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// User input carries the situation, operating instructions and regenerable planning todos. With a concrete change, point to it; on a heartbeat/hint/resume without one, do not falsely claim graph changes and review current intents instead.
	lead := "Concrete changes occurred (see Changes that triggered this round below). Plan the next step:"
	if len(triggers) == 0 {
		lead = "This is a periodic heartbeat wake with no concrete change signal; the graph may be unchanged. Review running intents: use steer_work for stalled or drifting work and kill_work for entirely wrong directions. Then evaluate goals and decide whether to add directions:"
		// If a no-change wake finds neither open nor running intents, explicitly identify stalled exploration and require a new direction rather than an empty review round.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "This is a periodic heartbeat wake with no open or running intents. No worker is running and no direction is queued. First determine from the situation below whether goals are met. If unmet, you must produce at least one nonduplicate intent that advances them; zero intents would leave exploration stalled:"
		}
	}
	input := lead + situational + "\n\nEvaluate goals using the situation above. Use prove_goal individually only when the required outcome or vulnerability is actually established. Mandatory progress rule: while a goal remains unmet and frontier_open=0 with no running_intents, produce at least one intent advancing the goal. There is no running or queued work to wait for. Zero new intents is permitted only when open/running intents are already advancing the task or goals are met." +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration interrupts a stalled active tool at expiry and enters settlement on a live context. The outer context is reserved for pause, kill and shutdown.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}

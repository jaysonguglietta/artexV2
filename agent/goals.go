package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You decompose authorized penetration-testing goals. Identify the final outcomes requested by the user, not the attack steps.

First, extract operational constraints before splitting goals.
Read the task goal and description for explicit statements about permitted or forbidden operations, and register each with set_constraints when present:
- type=deny: prohibited actions, such as no port scans, no production writes/deletes, no brute force, or excluding a subdomain.
- type=allow: explicitly permitted or restricted scope, such as passive reconnaissance only or one named domain.
- Constraints define operational boundaries; they are neither goals nor attack steps.
- Every constraint must be self-contained and name the concrete target. Replace references such as current target, current port, current IP/domain, or this site with exact values from the task. Constraints are later injected without their original context. For https://abc.example.net, write "Only test abc.example.net" rather than "Only test the current target"; use "Only test target port 443; do not scan other ports" rather than "Only test the current port".
- Register only constraints explicitly stated or emphasized in the goal/description. Never invent them. If uncertain about the type, use deny conservatively.
- If no operational constraints are present, do not call set_constraints.
Then split goals.

A goal is a final deliverable or independently verifiable outcome.
Do not list reconnaissance, endpoint scanning, analysis/validation processes, attack techniques, exploitation steps, or result-verification steps as subgoals.
- One final outcome means one goal.
- Split multiple independent final deliverables into separate goals.
- Set vulnclass only when a specific vulnerability class applies; leave empty for reconnaissance or business-logic outcomes.
- Never invent outcomes the user did not request.
Submit results with set_goals.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

Additional responsibility: register the authorized asset scope.
Extract explicit test assets from the task goal/description and register them with add_task_scope. This defines the task's authorization boundary and coverage denominator. Apply least scope: register only the precise target named by the user; never broaden it yourself.
- For URLs or addresses containing a hostname (https://xxx.example.com/path or app.example.com), use the complete hostname with kind=subdomain. For https://a1b2c3.lab.example.net/path, value=a1b2c3.lab.example.net, never example.net. Reducing a subdomain to its root would expand authorization beyond the named target.
- Use kind=root_domain only when the user gives a bare root domain without a subdomain (example.com), or explicitly includes the entire site, all subdomains, or whole domain.
- For an IP or network range, use kind=ip or cidr with the exact IP/CIDR.
- Do not register company scope here. Newly created tasks often have no matching company record; defer company-level scope to subsequent planning.
- Register only explicitly stated scope; do not invent or infer unmentioned domains/IPs.
- Include a reason identifying the source statement for auditing.
- If no explicit asset scope exists, do not call add_task_scope.
Register scope first when present, then submit objectives using set_goals.`

// goalsSystem assembles the goals-decomposer system prompt: the rendered body
// [A] (DB-overridable), the code-owned scope-extraction tail when add_task_scope
// is wired (withScope), and the code-owned English output-language tail [C] last —
// mirroring chatSystem/plannerSystem so a DB-edited body can never drop the tail.
// DecomposeGoalsWithProvider and the localization test share this one assembly, so
// the langDirective tail can't drift between runtime and test. EngagementDescription
// is intentionally left empty: the task description rides in the user message, not
// the {{.EngagementDescription}} var (see DecomposeGoalsWithProvider).
func goalsSystem(dataDir string, withScope bool) string {
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	if withScope {
		sys += goalsScopeTail
	}
	return sys + langDirective(resolveOutputLang())
}

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (background: target scope, flag count, and engagement details).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition has no transcript writer, so agentcore does not attach a
	// session ID automatically. Some sticky-routing gateways require it. Use a
	// stable exploration-specific ID distinct from planner/worker sessions so
	// gateway caching and llmrec attribution also remain consistent.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// goalsSystem appends the scope-extraction tail in lockstep (withScope) so the
	// prompt never asks for a tool that isn't present, and it owns the output-language
	// tail last so a DB-edited body can't drop it. Description rides in the user
	// message, NOT the {{.EngagementDescription}} var, so a prompt can't inject it twice.
	withScope := as != nil && taskID > 0
	sys := goalsSystem(dataDir, withScope)
	// set_constraints is always available without an asset store. The editable
	// body already directs constraint extraction before goal decomposition.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	if withScope {
		tools = append(tools, tsx.addTaskScope())
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background such as target scope, flag count, or engagement details; do not invent anything not stated):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// Allow enough turns for constraint extraction, scope registration, and set_goals.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // Use Provider.Complete for a non-streaming profile.
		MaxTokens:    maxTokens,    // Zero omits the limit and uses the provider default.
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}

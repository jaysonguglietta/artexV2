package agent

import (
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Settlement prompts run after the agent reaches MaxTurns or MaxDuration.
// They save identified results before producing the final summary.
//
// Admins may override agents.wrapup_prompt; empty uses the defaults below.
// Tool restrictions remain code-owned; turn budgets are resolved separately.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in prompts by agent key. The worker uses settleWrapUpPrompt in
// worker.go; unregistered/custom agents use the generic fallback.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// Default settlement turn budgets; positive admin overrides take precedence.
// Ten turns allow results to be saved; unknown agents use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "The step budget for this planning round is about to expire. Only this round ends; the system will wake you again as the situation changes. The task itself is not ending. Apply conclusions already reached, without inventing intents merely to fill the wrap-up; zero intents is legitimate when already covered or awaiting dependencies. (1) Submit directions already ready for dispatch in one add_intent call. (2) Mark goals already proven by findings/facts with prove_goal. (3) Record sequential chains with TodoWrite for future wakes. Then end this round immediately without summary text."

const mainAgentWrapUpDefault = "This interaction is about to end because its step budget is exhausted. Do not start new investigation or operations. Output only one sentence of plain text in English summarizing progress, the key conclusion, and the recommended next step."

const genericWrapUpDefault = "The budget is exhausted and this run is ending. Save completed results not yet recorded, then output only one sentence of plain text in English stating what you did and the key conclusion. That sentence is displayed as the run result."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        withWrapupLang(resolveWrapup(agentKey)),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// withWrapupLang appends the code-owned English output policy to settlement
// instructions. Saved overrides must also preserve raw technical evidence.
func withWrapupLang(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	lang := resolveOutputLang()
	return prompt + langDirective(lang)
}

// Task-level timeout settlement
//
// Run-budget and task-timeout prompts have different meanings. A planner may
// continue planning after a run budget, but must stop creating work at the task
// deadline and perform final evaluation. Only worker/planner have timeout prompts.

// Database overrides for agents.task_timeout_wrapup_prompt and its turn budget,
// used only by worker/planner.
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "The entire task has reached its time limit and is about to end, not merely this run. This is the final opportunity: (1) Save all identified but unrecorded results using insert_assets for assets, record_fact for facts/conclusions, and report_finding for confirmed vulnerabilities. (2) Do not start any new commands or probes. (3) Finish with only one sentence of plain text in English summarizing this intent's key conclusion."

const plannerTaskTimeoutDefault = "The entire task has reached its time limit and is ending, not merely this planning round. Perform the final goal evaluation using all facts and findings collected so far. Mark goals proven by evidence with prove_goal. Do not create any new intents; they will not execute. End immediately after evaluation without summary text."

// TaskTimeoutWrapupDefault provides the timeout placeholder/restore-default value.
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // Empty for unconfigured mainagent/chat.
}

// A nonempty DB override takes precedence over the built-in timeout prompt.
// Empty means unavailable for this role; callers fall back to the run prompt.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // Default to the run settlement budget.
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true: the task deadline limits this run; Timeout uses task settlement,
//     while MaxTurns uses run settlement because task time may remain.
//   - clamped=false: both reasons use run settlement, as in wrapupSettlement.
//
// PromptByReason selects using the actual settlement reason at runtime.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := withWrapupLang(resolveWrapup(agentKey))
	st := &harness.Settlement{
		Prompt:        perRun, // Fallback, and both reasons when not clamped.
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  withWrapupLang(tt), // Task deadline reached.
				harness.ReasonMaxTurns: perRun,             // Run steps exhausted while task time remains.
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}

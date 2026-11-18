package agent

import (
	"strings"
	"testing"
	"unicode"
)

// Wrap-up prompts are English code-owned defaults for run/task turn and time settlement. Preserve tool identifiers and semantic instructions, reject untranslated CJK narrative, and keep one-sentence plain-text summaries where required. Empty database wrapup fields fall back to these constants.
func TestWrapupPromptsUseEnglish(t *testing.T) {
	all := map[string]string{
		"settleWrapUpPrompt":        settleWrapUpPrompt,
		"plannerWrapUpDefault":      plannerWrapUpDefault,
		"mainAgentWrapUpDefault":    mainAgentWrapUpDefault,
		"genericWrapUpDefault":      genericWrapUpDefault,
		"workerTaskTimeoutDefault":  workerTaskTimeoutDefault,
		"plannerTaskTimeoutDefault": plannerTaskTimeoutDefault,
	}

	hasScript := func(s string, table *unicode.RangeTable) bool {
		for _, r := range s {
			if unicode.Is(table, r) {
				return true
			}
		}
		return false
	}

	for name, p := range all {
		if hasScript(p, unicode.Hangul) {
			t.Errorf("%s: contains unexpected Hangul text", name)
		}
		// Tool identifiers stay ASCII and translated narrative must contain no Han characters.
		if hasScript(p, unicode.Han) {
			t.Errorf("%s: contains untranslated Han text: %q", name, p)
		}
	}

	// Preserve record_fact/report_finding instructions and the final plain-text sentence for worker settlement.
	mustContain := func(name, p string, subs ...string) {
		for _, s := range subs {
			if !strings.Contains(p, s) {
				t.Errorf("%s: required instruction %q is missing", name, s)
			}
		}
	}
	mustContain("settleWrapUpPrompt", settleWrapUpPrompt,
		"insert_assets", "record_fact", "report_finding", "one sentence", "plain text")
	mustContain("workerTaskTimeoutDefault", workerTaskTimeoutDefault,
		"insert_assets", "record_fact", "report_finding", "one sentence", "plain text")
	mustContain("genericWrapUpDefault", genericWrapUpDefault, "one sentence", "plain text")
	mustContain("mainAgentWrapUpDefault", mainAgentWrapUpDefault, "one sentence", "plain text")
	// Planner settlement evaluates/persists work using intent/goal/todo tools rather than a user summary.
	mustContain("plannerWrapUpDefault", plannerWrapUpDefault, "add_intent", "prove_goal", "TodoWrite")
	mustContain("plannerTaskTimeoutDefault", plannerTaskTimeoutDefault, "prove_goal")
}

// Distinguish a single planning round ending from the entire task timing out; their prompt mappings must not be swapped.
func TestWrapupDefaultsRouting(t *testing.T) {
	if WrapupDefault("worker") != settleWrapUpPrompt {
		t.Error("worker per-run default is not settleWrapUpPrompt")
	}
	if WrapupDefault("planner") != plannerWrapUpDefault {
		t.Error("planner per-run default is not plannerWrapUpDefault")
	}
	if WrapupDefault("mainagent") != mainAgentWrapUpDefault {
		t.Error("mainagent per-run default is not mainAgentWrapUpDefault")
	}
	// Unknown custom-agent keys use the generic default.
	if WrapupDefault("unknown-agent") != genericWrapUpDefault {
		t.Error("unregistered key did not use genericWrapUpDefault")
	}
	// Only worker/planner have task-timeout variants; other roles return empty and callers use per-run settlement.
	if TaskTimeoutWrapupDefault("worker") != workerTaskTimeoutDefault {
		t.Error("worker task-timeout default is not workerTaskTimeoutDefault")
	}
	if TaskTimeoutWrapupDefault("planner") != plannerTaskTimeoutDefault {
		t.Error("planner task-timeout default is not plannerTaskTimeoutDefault")
	}
	if TaskTimeoutWrapupDefault("mainagent") != "" {
		t.Error("mainagent must have no task-timeout prompt")
	}
	// Identical per-run and task-timeout prompts would lose their semantic distinction.
	if workerTaskTimeoutDefault == settleWrapUpPrompt {
		t.Error("worker per-run and task-timeout prompts are identical")
	}
	if plannerTaskTimeoutDefault == plannerWrapUpDefault {
		t.Error("planner per-run and task-timeout prompts are identical")
	}
}

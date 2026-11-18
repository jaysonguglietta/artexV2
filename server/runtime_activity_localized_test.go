package server

import (
	"fmt"
	"strings"
	"testing"
)

// Task-scoped runtime activity summaries have no node_id and are not selected by intent-scoped worker-output feedback. Pin their English display text while preserving formatting arguments.
func TestRuntimeActivitySummariesLocalized(t *testing.T) {
	// Fixed summaries without format arguments must be English.
	plain := map[string]string{
		"goalless_task_done": goallessTaskDoneSummary,
		"goal_breakdown_r0":  goalBreakdownRound0Summary,
		"queued_no_llm":      queuedNoLLMSummary,
		"queued_fifo":        queuedFIFOSummary,
	}
	for label, msg := range plain {
		assertEnglishError(t, label, msg)
	}

	// Preserve %d formatting and verify the representative numeric value appears in the English result.
	fmtCases := map[string]string{
		"planner_round":       plannerRoundSummaryFmt,
		"timeout_final_round": timeoutFinalRoundSummaryFmt,
		"queued_conc_limit":   queuedConcurrencyLimitSummaryFmt,
	}
	for label, f := range fmtCases {
		if !strings.Contains(f, "%d") {
			t.Fatalf("%s: format argument %%d disappeared: %q", label, f)
		}
		got := fmt.Sprintf(f, 7)
		assertEnglishError(t, label, got)
		if !strings.Contains(got, "7") {
			t.Fatalf("%s: format argument is absent from the result: %q", label, got)
		}
	}
}

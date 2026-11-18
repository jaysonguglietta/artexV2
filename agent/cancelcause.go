package agent

import (
	"context"
	"errors"
	"fmt"
)

// AbortCause names why an agent run's context was cancelled. Every cancellation
// site should attach one so the activity trace can report the real initiator.
type AbortCause struct {
	Code  string
	Short string
	Text  string
}

func (c *AbortCause) Error() string { return c.Text }

func cause(code, short, text string) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: text}
}

// Causef builds a cause that includes runtime-specific detail.
func Causef(code, short, format string, args ...any) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: fmt.Sprintf(format, args...)}
}

var (
	// Task-level execution context.
	AbortPausedByUser = cause("paused_by_user", "User paused the task",
		"The user paused this task through POST /api/tasks/{id}/control with action=pause. Planner/worker runs were canceled; running intents return to frontier(open) and restart from the beginning when resumed.")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "Orchestrator paused the task",
		"The orchestration agent called pause_task. Planner/worker runs were canceled; running intents return to frontier(open) and restart after resumption.")
	AbortTaskDeleted = cause("task_deleted", "Task deleted",
		"DELETE /api/tasks/{id} is deleting this task. The deletion barrier canceled its running planner, workers, and main agent; their results will no longer be used.")
	AbortPausedOnReload = cause("paused_on_reload", "Task pause restored on startup",
		"Startup restored the persisted paused task state and canceled this run. Normally no agents should be running during restoration.")
	AbortGoalMet = cause("goal_met", "Planner marked the goal met",
		"The planner marked the task done after determining its goal was met, then canceled remaining workers. Their intents become stopped, not failed.")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "Task settlement drain expired",
		"After the task timeout, the 90-second drain grace period was insufficient for graceful worker settlement. Hard cancellation marks intents exhausted; facts and assets already saved remain.")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "Planner stopped this intent",
		"The planner called kill_work, usually because the direction drifted or had no continuing value. The intent becomes stopped and is not automatically reclaimed.")
	AbortWorkPausedByUser = cause("work_paused_by_user", "User paused this worker intent",
		"The user paused the running worker. This call was canceled and the intent becomes paused. Recorded intents, facts, findings, and activities remain; resumption restarts execution from the beginning.")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "User deleted this worker intent",
		"The user deleted the running worker. After cancellation exits the write section, soft deletion marks the intent deleted while retaining outputs; hard deletion cascades to the intent and downstream nodes supported only by it.")
	AbortWorkFinished = cause("work_finished", "Worker finished; context released",
		"The worker completed normally and detachWork released its context. This is not an interruption; if shown as one, cancellation raced with completion.")
	AbortPausedRaceGuard = cause("paused_race_guard", "New run rejected while paused",
		"The engine refused a new execution context while the task was paused, preventing a claim/pause race from starting a worker. The claimed intent returns to the frontier.")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "User stopped this conversation turn",
		"The user clicked Stop to cancel the main-agent or conversation-agent turn. Existing activities remain and the user can send another message.")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "Task pause stopped the main-agent chat",
		"Pausing the task also canceled its running main-agent conversation. Existing activities remain; resuming the task does not automatically replay this message.")
	AbortChatTurnFinished = cause("chat_turn_finished", "Conversation turn finished",
		"The conversation turn completed normally and the server is releasing its context. This is not an interruption; if shown as one, cancellation raced with completion.")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "Backend is shutting down",
		"The backend received SIGINT or SIGTERM for restart, update, or shutdown and canceled running agents. After restart, remaining running intents reset to open for execution.")
	AbortRunHardTimeout = cause("run_hard_timeout", "Run hard-timeout backstop fired",
		"The run exceeded its soft wall-clock budget plus grace period, indicating a model request or tool did not return and normal settlement could not start. Inspect the last unfinished tool call.")
)

// AbortReason resolves the named cause attached to a cancelled run context.
func AbortReason(ctx context.Context) (code, short, text string, ok bool) {
	c := context.Cause(ctx)
	if c == nil {
		return "", "", "", false
	}
	var ac *AbortCause
	if errors.As(c, &ac) {
		return ac.Code, ac.Short, ac.Text, true
	}
	switch {
	case errors.Is(c, context.DeadlineExceeded):
		return "deadline_exceeded", "Upstream context deadline reached",
			"The upstream context reached its deadline without a named WithTimeoutCause: " + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", "Cancellation has no named cause",
			"The upstream context was canceled without a named context.WithCancelCause. Register the cause in agent/cancelcause.go and attach it at the cancellation site", true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}

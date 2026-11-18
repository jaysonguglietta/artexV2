package server

import "testing"

// Pin English resume precondition errors behind concurrency/engine gates. Single and batch task-control responses share these messages; orchestration pause does not enter resume validation.
func TestGoalResumeErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"resume_terminal", errGoalResumeTerminal},
		{"resume_not_paused", errGoalResumeNotPaused},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}

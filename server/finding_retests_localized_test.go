package server

import "testing"

// Pin English retest HTTP errors behind the database gate. Tool schemas/results and seeded agent metadata are independently reviewed narrative; protocol values and actual evidence remain unchanged.
func TestFindingRetestErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"notes_too_long", errFindingRetestNotesTooLong},
		{"agent_missing", errFindingRetestAgentMissing},
		{"tool_required", errFindingRetestToolRequired},
		{"service_stopping", errFindingRetestServiceStopping},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}

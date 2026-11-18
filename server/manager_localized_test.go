package server

import "testing"

// Exercise English SetWorkers validation on an empty Manager: nonpositive counts return before database access. The error reaches the settings HTTP response and must retain its validation meaning.
func TestSetWorkersErrorLocalized(t *testing.T) {
	err := (&Manager{}).SetWorkers(0)
	if err == nil {
		t.Fatal("Zero worker count must be rejected")
	}
	assertEnglishError(t, "set_workers_nonpositive", err.Error())
}

package server

import "testing"

// Pin the reporter display name/description as English UI labels. Routing uses the stable reporter key and deterministic tool-call trigger; narrative prompt/trigger bodies and legacy migration comparisons are separate contracts.
func TestReporterSeedLabelsLocalized(t *testing.T) {
	assertEnglishError(t, "reporter_name", reporterAgentName)
	assertEnglishError(t, "reporter_description", reporterAgentDescription)
}

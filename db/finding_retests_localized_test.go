package db

import (
	"testing"
	"unicode"
)

// These reasons are persisted in finding_retests.error and shown directly in the
// retest panel. Keep generated explanations English, preserving sentinel behavior.
func assertRetestReasonEnglish(t *testing.T, name, message string) {
	t.Helper()
	if message == "" {
		t.Fatalf("%s: empty reason", name)
	}
	for _, r := range message {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) {
			t.Fatalf("%s: untranslated reason: %q", name, message)
		}
	}
}

func TestFindingRetestReasonsUseEnglish(t *testing.T) {
	assertRetestReasonEnglish(t, "retestNoConclusionReason", retestNoConclusionReason)
	assertRetestReasonEnglish(t, "retestServiceRestartReason", retestServiceRestartReason)
}

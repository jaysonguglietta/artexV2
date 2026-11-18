package server

import (
	"net/url"
	"testing"
	"unicode"
)

// assertEnglishError fails when msg is empty, still carries a CJK Han ideograph
// (= untranslated Chinese), or carries no Hangul at all. Hangul and ASCII field
// names/enums are allowed; only Han (unicode.Han) marks a leftover Chinese string.
func assertEnglishError(t *testing.T, label, msg string) {
	t.Helper()
	if msg == "" {
		t.Fatalf("%s: empty message", label)
	}
	for _, r := range msg {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			t.Fatalf("%s: expected English message: %q", label, msg)
		}
	}
}

// TestInterceptArchiveErrorsLocalized guards the F3 first bundle: every
// user-facing error response produced by the pure validators in intercept.go and
// task_archives.go must be Korean (Hangul present, no Chinese Han). If anyone
// reverts one of these literals to Chinese, this test fails.
func TestInterceptArchiveErrorsLocalized(t *testing.T) {
	// intercept.go: interceptFilterParams — query-string validation.
	if _, err := interceptFilterParams(url.Values{"status": {"bogus"}}); err == nil {
		t.Fatal("Invalid status must not pass validation")
	} else {
		assertEnglishError(t, "filter.status", err.Error())
	}
	if _, err := interceptFilterParams(url.Values{"decision_source": {"bogus"}}); err == nil {
		t.Fatal("Invalid decision_source must not pass validation")
	} else {
		assertEnglishError(t, "filter.decision_source", err.Error())
	}

	// intercept.go: validateInterceptRuleReq — each branch isolated by making the
	// earlier fields valid.
	ruleCases := []struct {
		label string
		req   interceptRuleReq
	}{
		{"rule.name", interceptRuleReq{Name: ""}},
		{"rule.match_target", interceptRuleReq{Name: "r", MatchTarget: "bad"}},
		{"rule.match_type", interceptRuleReq{Name: "r", MatchTarget: "tool_name", MatchType: "bad"}},
		{"rule.pattern_empty", interceptRuleReq{Name: "r", MatchTarget: "tool_name", MatchType: "string", Pattern: ""}},
		{"rule.action", interceptRuleReq{Name: "r", MatchTarget: "tool_name", MatchType: "string", Pattern: "p", Action: "bad"}},
		{"rule.regex", interceptRuleReq{Name: "r", MatchTarget: "tool_name", MatchType: "regex", Pattern: "(", Action: "allow"}},
	}
	for _, tc := range ruleCases {
		err := validateInterceptRuleReq(tc.req)
		if err == nil {
			t.Fatalf("%s: validation unexpectedly passed", tc.label)
		}
		assertEnglishError(t, tc.label, err.Error())
	}

	// task_archives.go: normalizeArchiveIDs — batch id validation.
	if _, err := normalizeArchiveIDs(nil); err == nil {
		t.Fatal("Empty archive_ids must not pass validation")
	} else {
		assertEnglishError(t, "archive_ids.empty", err.Error())
	}
	tooMany := make([]int64, 101)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	if _, err := normalizeArchiveIDs(tooMany); err == nil {
		t.Fatal("101 archive IDs must not pass validation")
	} else {
		assertEnglishError(t, "archive_ids.too_many", err.Error())
	}
	if _, err := normalizeArchiveIDs([]int64{0}); err == nil {
		t.Fatal("Zero archive ID must not pass validation")
	} else {
		assertEnglishError(t, "archive_ids.nonpositive", err.Error())
	}

	// task_archives.go: validateArchivePath — package path validation.
	if err := validateArchivePath("/tmp/artex-data", ""); err == nil {
		t.Fatal("Empty archive path must not pass validation")
	} else {
		assertEnglishError(t, "archive_path.empty", err.Error())
	}
	if err := validateArchivePath("/tmp/artex-data", "/etc/passwd"); err == nil {
		t.Fatal("A path outside the management directory must not pass validation")
	} else {
		assertEnglishError(t, "archive_path.outside", err.Error())
	}
}

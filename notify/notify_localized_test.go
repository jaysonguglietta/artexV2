package notify

import (
	"strings"
	"testing"
)

// hasHan detects untranslated Han characters in label framing.
func hasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// hasHangul detects Hangul syllables.
func hasHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

// The English label assertion rejects untranslated Han/Hangul narrative.
func assertEnglish(t *testing.T, label, msg string) {
	t.Helper()
	if msg == "" || hasHan(msg) || hasHangul(msg) {
		t.Errorf("%s: expected a nonempty English message: %q", label, msg)
	}
}

// Test every severity label shared by all notification adapters.
func TestSeverityLabelLocalized(t *testing.T) {
	// English severity labels stay aligned with UI terminology.
	want := map[string]string{
		"critical": "Critical",
		"high":     "High",
		"medium":   "Medium",
		"low":      "Low",
	}
	for sev, label := range want {
		got := SeverityLabel(sev)
		assertEnglish(t, "SeverityLabel("+sev+")", got)
		if !strings.Contains(got, label) {
			t.Errorf("SeverityLabel(%q)=%q, should contain %q", sev, got, label)
		}
	}
	// Unknown severities pass through unchanged.
	if got := SeverityLabel("made_up"); got != "made_up" {
		t.Errorf("unknown severity should be unchanged, got %q", got)
	}
}

// Test all nine English disposition labels against UI status terminology.
func TestStatusLabelLocalized(t *testing.T) {
	want := map[string]string{
		"pending":        "Pending",
		"in_progress":    "In flight",
		"confirmed":      "Confirmed",
		"resolved":       "Resolved",
		"fixed":          "Fixed",
		"false_positive": "False positive",
		"ignored":        "Ignored",
		"duplicate":      "Duplicate",
		"risk_accepted":  "Risk accepted",
	}
	for status, label := range want {
		got := StatusLabel(status)
		assertEnglish(t, "StatusLabel("+status+")", got)
		if got != label {
			t.Errorf("StatusLabel(%q)=%q, want %q", status, got, label)
		}
	}
	// Unknown statuses pass through unchanged.
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Errorf("unknown status should be unchanged, got %q", got)
	}
}

// An unnamed/unclassified finding still has a nonempty English title.
func TestItemTitlePlaceholderLocalized(t *testing.T) {
	got := Item{}.Title()
	assertEnglish(t, "Item{}.Title()", got)
	// Prefer assigned names, then classes, before the placeholder.
	if n := (Item{Name: "로그인 SQLi"}).Title(); n != "로그인 SQLi" {
		t.Errorf("name precedence failed, got %q", n)
	}
	if v := (Item{VulnClass: "XSS"}).Title(); v != "XSS" {
		t.Errorf("class fallback failed, got %q", v)
	}
}

// Test ASCII list separators and English total-count wording.
func TestAssetLineLocalized(t *testing.T) {
	// Within the limit, list every asset with ASCII commas.
	if got := assetLine([]string{"a.example.com", "b.example.com"}, 3); got != "a.example.com, b.example.com" {
		t.Errorf("separator should be an ASCII comma, got %q", got)
	}
	// Above the limit, list the first entries and the total asset count.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	if hasHan(got) {
		t.Errorf("asset list contains Han text: %q", got)
	}
	if !strings.Contains(got, "(5 total)") {
		t.Errorf("total should be rendered as (5 total), got %q", got)
	}
}

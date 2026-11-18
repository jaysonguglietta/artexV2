package notify

import (
	"strings"
	"testing"
)

// Test English email HTML labels and SMTP validation errors. ASCII fixture data isolates translated framing from original target evidence.

// Exercise every single-email label branch and reject untranslated Han narrative.
func TestHTMLItemLabelsLocalized(t *testing.T) {
	m := Message{
		HomeURL: "https://example.com/panel",
		Items: []Item{{
			Name:       "sqli-login", // Title() = Name
			VulnClass:  "injection",  // Use a class different from the title so its row renders.
			Severity:   "high",       // English high-severity label.
			Summary:    "login form is injectable",
			Assets:     []string{"host-a.example.com"},
			DetailURL:  "https://example.com/f/1",
			FromStatus: "pending", // Status-change event enables its transition row.
			ToStatus:   "fixed",
		}},
	}
	out := htmlBody(m, 0)
	if hasHan(out) {
		t.Errorf("email body contains untranslated Han text:\n%s", out)
	}
	for _, want := range []string{
		"Status changed", "Type", "Assets", "Overview", "View details", "View all in ARTEX",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("email body lacks %q label:\n%s", want, out)
		}
	}
}

// Test digest introductions with and without a time window.
func TestHTMLBatchIntroLocalized(t *testing.T) {
	items := []Item{{Name: "a", Severity: "high"}, {Name: "b", Severity: "low"}}

	withWindow := htmlBatchIntro(Message{Batch: true, WindowMinutes: 30, Items: items})
	assertEnglish(t, "htmlBatchIntro(window)", withWindow)
	for _, want := range []string{"Last 30 minutes", "New findings", "2"} {
		if !strings.Contains(withWindow, want) {
			t.Errorf("window introduction requires %q: %q", want, withWindow)
		}
	}

	noWindow := htmlBatchIntro(Message{Batch: true, WindowMinutes: 0, Items: items})
	assertEnglish(t, "htmlBatchIntro(no window)", noWindow)
	if !strings.Contains(noWindow, "New findings: 2") {
		t.Errorf("introduction without window should contain %q: %q", "New findings: 2", noWindow)
	}
	if strings.Contains(noWindow, "minutes") {
		t.Errorf("windowless introduction unexpectedly contains minutes: %q", noWindow)
	}
}

// Each required SMTP-field validation error is English and identifies the missing setting.
func TestEmailValidateLocalized(t *testing.T) {
	cases := []struct {
		name   string
		cfg    map[string]any
		substr string
	}{
		{"missing server address", map[string]any{"port": float64(25), "from": "a@b.c", "to": []any{"d@e.f"}}, "SMTP"},
		{"port out of range", map[string]any{"host": "h"}, "Port"},
		{"missing sender", map[string]any{"host": "h", "port": float64(25), "to": []any{"d@e.f"}}, "Sender"},
		{"missing recipients", map[string]any{"host": "h", "port": float64(25), "from": "a@b.c"}, "Recipients"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (emailChannel{}).Validate(tc.cfg)
			if err == nil {
				t.Fatalf("validation should fail: %v", tc.cfg)
			}
			assertEnglish(t, "Validate("+tc.name+")", err.Error())
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error should contain %q, got %q", tc.substr, err.Error())
			}
		})
	}
}

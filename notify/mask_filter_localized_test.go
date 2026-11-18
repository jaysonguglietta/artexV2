package notify

import (
	"errors"
	"strings"
	"testing"
)

// Test English channel-config/filter validation errors returned by the API. Preserve field names and sentinel identifiers rather than translating protocol data.

// Test invalid minimum-severity guidance from the save/update path.
func TestFilterValidateLocalized(t *testing.T) {
	err := Filter{MinSeverity: "hgih"}.Validate()
	if err == nil {
		t.Fatal("invalid severity produced no error")
	}
	assertEnglish(t, "Filter.Validate", err.Error())
	// Keep low/medium/high/critical enum identifiers exact.
	for _, tok := range []string{"low", "medium", "high", "critical"} {
		if !strings.Contains(err.Error(), tok) {
			t.Errorf("severity identifier %q missing: %q", tok, err.Error())
		}
	}
	// Accept supported and empty thresholds.
	if err := (Filter{MinSeverity: "high"}).Validate(); err != nil {
		t.Errorf("valid high threshold rejected: %v", err)
	}
	if err := (Filter{}).Validate(); err != nil {
		t.Errorf("valid empty threshold rejected: %v", err)
	}
}

// Test the English unknown-channel-kind update error.
func TestPrepareConfigUpdateUnknownKindLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("definitely-not-a-channel", map[string]any{}, map[string]any{})
	if err == nil {
		t.Fatal("unknown channel produced no error")
	}
	assertEnglish(t, "PrepareConfigUpdate unknown kind", err.Error())
}

// Exercise the destination-only webhook update attack and its missing-credentials error.
func TestDestinationChangedErrorLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("webhook",
		map[string]any{
			"url":     "https://old.example.com/hook",
			"headers": map[string]any{"Authorization": "Bearer real-token"},
		},
		map[string]any{"url": "https://attacker.example.com/hook"},
	)
	if err == nil {
		t.Fatal("destination change without credential decision produced no error")
	}
	var de *ErrDestinationChangedWithoutCredentials
	if !errors.As(err, &de) {
		t.Fatalf("unexpected error type: %T", err)
	}
	assertEnglish(t, "ErrDestinationChangedWithoutCredentials", err.Error())
	// Keep changed destination and missing credential key names exact.
	if !strings.Contains(err.Error(), "url") || !strings.Contains(err.Error(), "headers") {
		t.Errorf("configuration field name missing: %q", err.Error())
	}
}

// Exercise rejection of masked values nested inside a headers object.
func TestRejectMaskedInContainersLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("webhook",
		map[string]any{},
		map[string]any{"headers": map[string]any{"Authorization": MaskedPrefix + ":…abc123"}},
	)
	if err == nil {
		t.Fatal("nested masked sentinel produced no error")
	}
	assertEnglish(t, "rejectMaskedInContainers", err.Error())
	// Include the exact __masked__ sentinel in corrective guidance.
	if !strings.Contains(err.Error(), MaskedPrefix) {
		t.Errorf("masked sentinel missing from error: %q", err.Error())
	}
}

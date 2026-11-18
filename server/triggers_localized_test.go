package server

import "testing"

// English trigger-validation regression coverage.

// Pin all three error constants.
func TestTriggerErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"no_condition", errTriggerNoCondition},
		{"tool_set_empty", errTriggerToolSetEmpty},
		{"custom_only", errTriggerCustomOnly},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// Exercise the pure validator: missing conditions/tools return the English constants, valid input returns empty. CRUD handlers display these results directly as HTTP 400.
func TestValidateTriggerLocalized(t *testing.T) {
	if msg := validateTrigger(&triggerReq{}); msg != errTriggerNoCondition {
		t.Fatalf("Missing-condition message=%q, want %q", msg, errTriggerNoCondition)
	}
	if msg := validateTrigger(&triggerReq{OnToolCall: true}); msg != errTriggerToolSetEmpty {
		t.Fatalf("Missing-tool message=%q, want %q", msg, errTriggerToolSetEmpty)
	}
	if msg := validateTrigger(&triggerReq{OnFinding: true}); msg != "" {
		t.Fatalf("Valid input message=%q, want empty", msg)
	}
	if msg := validateTrigger(&triggerReq{OnToolCall: true, ToolNames: []string{"nmap"}}); msg != "" {
		t.Fatalf("Valid tool-call message=%q, want empty", msg)
	}
}

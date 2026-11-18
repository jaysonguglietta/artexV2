package server

import "testing"

// Regression coverage for English global/task asset-rule validation. Pure validators permit direct error-path checks without a database; preserve protocol fields and enum values.
func TestAssetInterceptErrorsLocalized(t *testing.T) {
	// Pin the error constants.
	for _, c := range []struct{ label, msg string }{
		{"pattern_empty", errAssetInterceptPatternEmpty},
		{"bad_exact_ip", errAssetInterceptInvalidExactIPFmt},
		{"bad_cidr", errAssetInterceptInvalidCIDRFmt},
		{"bad_kind", errAssetInterceptInvalidKindFmt},
		{"bad_action", errTaskInterceptInvalidAction},
	} {
		assertEnglishError(t, c.label, c.msg)
	}

	// Exercise all four global-rule rejection paths.
	for _, c := range []struct {
		label string
		req   assetInterceptRuleReq
	}{
		{"empty_pattern", assetInterceptRuleReq{Kind: "exact_ip", Pattern: "   "}},
		{"invalid_exact_ip", assetInterceptRuleReq{Kind: "exact_ip", Pattern: "not-an-ip"}},
		{"invalid_cidr", assetInterceptRuleReq{Kind: "cidr", Pattern: "nonsense"}},
		{"invalid_kind", assetInterceptRuleReq{Kind: "nope", Pattern: "example.com"}},
	} {
		req := c.req
		err := validateAssetInterceptRuleReq(&req)
		if err == nil {
			t.Fatalf("%s: expected validation error", c.label)
		}
		assertEnglishError(t, c.label, err.Error())
	}

	// Valid input remains accepted.
	okReq := assetInterceptRuleReq{Kind: "cidr", Pattern: "192.168.0.0/16"}
	if err := validateAssetInterceptRuleReq(&okReq); err != nil {
		t.Fatalf("Valid CIDR was rejected: %v", err)
	}

	// Exercise the task-rule action rejection path.
	badAction := taskInterceptRuleReq{Action: "nope", Kind: "exact_domain", Pattern: "example.com"}
	if err := validateTaskInterceptRuleReq(&badAction); err == nil {
		t.Fatalf("bad_action_path: expected validation error")
	} else {
		assertEnglishError(t, "bad_action_path", err.Error())
	}

	// An empty action normalizes to the default block action.
	defaultAction := taskInterceptRuleReq{Action: "", Kind: "exact_domain", Pattern: "example.com"}
	if err := validateTaskInterceptRuleReq(&defaultAction); err != nil {
		t.Fatalf("Default action normalization failed: %v", err)
	}
	if defaultAction.Action != "block" {
		t.Fatalf("Default action was not normalized to block: %q", defaultAction.Action)
	}
}

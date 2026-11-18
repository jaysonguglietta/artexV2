package notify

import "testing"

func TestParseFilterMalformedFallsBackToMatchAll(t *testing.T) {
	// Malformed/empty/type-invalid JSON falls back to the zero-value unfiltered configuration, preferring extra notification over silently lost critical events.
	cases := []struct {
		name string
		raw  string
	}{
		{"empty input", ""},
		{"invalid JSON", `{not json`},
		{"truncated JSON", `{"min_severity":`},
		{"wrong field types", `{"min_severity": 123, "task_ids": "abc"}`},
		{"top-level array", `[1,2,3]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ParseFilter([]byte(tc.raw))
			if f.MinSeverity != "" || len(f.TaskIDs) != 0 || len(f.AssetIDs) != 0 {
				t.Fatalf("malformed config should produce zero Filter, got %+v", f)
			}
			// The zero filter matches every event.
			ev := Snapshot{Kind: EventFindingCreated, Severity: "low", VulnClass: "XSS"}
			if !Match(f, ev) {
				t.Fatal("zero Filter should match every event")
			}
		})
	}
}

func TestMatchSeverityThreshold(t *testing.T) {
	ev := func(sev string) Snapshot {
		return Snapshot{Kind: EventFindingCreated, Severity: sev}
	}
	cases := []struct {
		min    string
		sev    string
		expect bool
	}{
		{"", "low", true},
		{"", "critical", true},
		{"high", "critical", true},
		{"high", "high", true},
		{"high", "medium", false},
		{"high", "low", false},
		{"critical", "high", false},
		{"critical", "critical", true},
		// Unknown severity rank zero fails every nonempty threshold.
		{"low", "", false},
		{"low", "unknown", false},
		{"", "", true},
	}
	for _, tc := range cases {
		got := Match(Filter{MinSeverity: tc.min}, ev(tc.sev))
		if got != tc.expect {
			t.Errorf("min=%q sev=%q: want %v, got %v", tc.min, tc.sev, tc.expect, got)
		}
	}
}

func TestMatchScopeRestrictions(t *testing.T) {
	ev := Snapshot{
		Kind:      EventFindingCreated,
		Severity:  "high",
		TaskID:    7,
		AssetIDs:  []int64{10, 20},
		VulnClass: "SQL注入",
	}
	cases := []struct {
		name   string
		filter Filter
		expect bool
	}{
		{"empty scope is unrestricted", Filter{}, true},
		{"task matches", Filter{TaskIDs: []int64{7}}, true},
		{"task does not match", Filter{TaskIDs: []int64{8}}, false},
		{"multiple tasks include match", Filter{TaskIDs: []int64{8, 7}}, true},
		{"assets intersect", Filter{AssetIDs: []int64{20, 99}}, true},
		{"assets do not intersect", Filter{AssetIDs: []int64{99}}, false},
		{"task and asset match", Filter{TaskIDs: []int64{7}, AssetIDs: []int64{10}}, true},
		{"task matches but asset does not", Filter{TaskIDs: []int64{7}, AssetIDs: []int64{99}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Match(tc.filter, ev); got != tc.expect {
				t.Errorf("want %v, got %v", tc.expect, got)
			}
		})
	}
}

func TestMatchVulnClassKeywords(t *testing.T) {
	ev := func(class string) Snapshot {
		return Snapshot{Kind: EventFindingCreated, Severity: "high", VulnClass: class}
	}
	cases := []struct {
		name   string
		filter Filter
		class  string
		expect bool
	}{
		{"empty inclusion accepts all", Filter{}, "任意类型", true},
		{"inclusion matches", Filter{VulnClassInclude: []string{"SQL"}}, "SQL注入", true},
		{"inclusion does not match", Filter{VulnClassInclude: []string{"命令执行"}}, "SQL注入", false},
		{"any inclusion keyword matches", Filter{VulnClassInclude: []string{"命令执行", "SQL"}}, "SQL注入", true},
		{"case-insensitive", Filter{VulnClassInclude: []string{"sql"}}, "SQL注入", true},
		{"exclusion match rejects", Filter{VulnClassExclude: []string{"Information disclosure"}}, "Information disclosure", false},
		{"no exclusion match permits", Filter{VulnClassExclude: []string{"Information disclosure"}}, "SQL注入", true},
		// Exclusion wins over a simultaneous include match.
		{"exclusion precedes inclusion", Filter{
			VulnClassInclude: []string{"SQL"},
			VulnClassExclude: []string{"注入"},
		}, "SQL注入", false},
		// Ignore whitespace-only keywords rather than matching every string containing spaces.
		{"ignore blank keywords", Filter{VulnClassInclude: []string{"", "  "}}, "SQL注入", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Match(tc.filter, ev(tc.class)); got != tc.expect {
				t.Errorf("want %v, got %v", tc.expect, got)
			}
		})
	}
}

func TestMatchStatusChangeRequiresOptIn(t *testing.T) {
	ev := Snapshot{Kind: EventFindingStatusChanged, Severity: "critical", FromStatus: "pending", ToStatus: "fixed"}
	// Status notifications default off because finding discovery is the common expectation.
	if Match(Filter{MinSeverity: "low"}, ev) {
		t.Fatal("status-change event should be skipped without opt-in")
	}
	if !Match(Filter{OnStatusChange: true}, ev) {
		t.Fatal("status change should match after opt-in")
	}
	// Creation events are independent of on_status_change.
	created := Snapshot{Kind: EventFindingCreated, Severity: "critical"}
	if !Match(Filter{MinSeverity: "low"}, created) {
		t.Fatal("creation events must not depend on on_status_change")
	}
}

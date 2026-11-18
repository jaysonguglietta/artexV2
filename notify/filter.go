package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter is the notification_channels.filter JSONB contract. All fields are optional; zero values mean no filtering, including malformed-config fallback.
type Filter struct {
	// MinSeverity selects the minimum low/medium/high/critical threshold; empty disables it.
	MinSeverity string `json:"min_severity"`
	// Empty TaskIDs/AssetIDs impose no restriction; nonempty lists require an event intersection.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// Empty include lists accept all classes. Any exclude match wins over inclusion. Match case-insensitive substrings rather than user regexes that could silently break filtering.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange enables disposition-change events; it is meaningful only for realtime channels.
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter never returns an error. Malformed configuration falls back to an unfiltered channel, preferring extra notifications over silently missing a high-severity finding.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure the zero-value filter accepts all events.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity accepts known thresholds or empty for no threshold.
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate rejects restricted-field errors on writes. An unknown threshold otherwise compares against rank zero and silently sends every event; catch typos before saving. Reads remain tolerant so invalid historical values cannot make a channel unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("Invalid minimum severity %q. Choose low / medium / high / critical, or leave empty for no minimum", f.MinSeverity)
	}
	return nil
}

// Match never returns an error, favoring a match on internal anomalies. Evaluate event type, severity, task/asset scope, then class keywords.
func Match(f Filter, s Snapshot) bool {
	// Disposition changes require explicit opt-in because most users expect notifications for new findings rather than every state transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclusion wins even if an include keyword also matches.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// Both sets are small operator-selected lists, so linear scanning is simpler than building maps.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold checks case-insensitive substring membership.
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

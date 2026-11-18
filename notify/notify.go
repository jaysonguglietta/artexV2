// Package notify adapts finding notifications to IM and email services. It is a standard-library-only leaf package with no database/server dependency: callers supply configuration maps and Message values. Signing, truncation and filtering remain independently unit-testable. Channel instances are shared concurrently and must be stateless; never cache credentials in an adapter.
package notify

// Channel identifiers also define valid notification_channels.kind values. The server validates them without a database CHECK constraint so adapters remain extensible.
const (
	KindDingTalk = "dingtalk" // DingTalk custom robot.
	KindFeishu   = "feishu"   // Feishu/Lark custom robot.
	KindWeCom    = "wecom"    // WeCom group robot.
	KindWebhook  = "webhook"  // Generic webhook with configurable method, headers and JSON template.
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email.
)

// Event kinds correspond to notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind supplies a fallback when config kind is empty.
const InitKind = KindDingTalk

// severityRank maps severity to an ordinal. Unknown severities return zero and fail any nonempty minimum threshold, avoiding uncertain notification noise.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns zero for unknown severity.
func SeverityRank(severity string) int { return severityRank[severity] }

// Lang, when wired by the server, returns the active user-facing output language
// ("en"/"ko"/"zh"/"es"/"ja") for the strings this leaf package renders into push /
// email messages (severity and status labels). Kept as an injected func so the
// package stays standard-library-only: it does not import config/db/server. nil
// (unit tests / standalone) → labelLang falls back to "ko", preserving the Korean
// localization golden tests.
var Lang func() string

// Notification labels are English regardless of stale saved locale settings.
func labelLang() string { return "en" }

var severityLabels = map[string]map[string]string{
	"en": {"critical": "🔴 Critical", "high": "🟠 High", "medium": "🟡 Medium", "low": "🔵 Low"},
}
var statusLabels = map[string]map[string]string{
	"en": {"pending": "Pending", "in_progress": "In flight", "confirmed": "Confirmed", "resolved": "Resolved", "fixed": "Fixed", "false_positive": "False positive", "ignored": "Ignored", "duplicate": "Duplicate", "risk_accepted": "Risk accepted"},
}

// SeverityLabel returns an emoji-prefixed English display label; unknown values are preserved rather than fabricated.
func SeverityLabel(severity string) string {
	if v, ok := severityLabels[labelLang()][severity]; ok {
		return v
	}
	return severity
}

// StatusLabel supplies an English disposition label and preserves unknown values.
func StatusLabel(status string) string {
	if v, ok := statusLabels[labelLang()][status]; ok {
		return v
	}
	return status
}

// AtLeast accepts every severity when min is empty. Unknown severity has rank zero and fails any nonempty threshold.
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}

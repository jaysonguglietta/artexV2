package server

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// English notification settings and synthetic test-message coverage.

// Pin ten response constants and three test-message constants.
func TestNotifyErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"notifyErrBadJSON":         notifyErrBadJSON,
		"notifyErrKindInvalidFmt":  fmt.Sprintf(notifyErrKindInvalidFmt, "email / webhook"),
		"notifyErrNameMissing":     notifyErrNameMissing,
		"notifyErrNameEmpty":       notifyErrNameEmpty,
		"notifyErrModeInvalid":     notifyErrModeInvalid,
		"notifyErrRateNegative":    notifyErrRateNegative,
		"notifyErrChannelID":       notifyErrChannelID,
		"notifyErrKindUnregFmt":    fmt.Sprintf(notifyErrKindUnregFmt, "bogus"),
		"notifyErrDeliveryID":      notifyErrDeliveryID,
		"notifyErrChannelNotFound": notifyErrChannelNotFound,
		"notifyTestName":           notifyTestName,
		"notifyTestClass":          notifyTestClass,
		"notifyTestSummary":        notifyTestSummary,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// Exercise missing-channel to HTTP 404 mapping without a Server/database and verify its English response body.
func TestNotifyChannelLookupErrLocalized(t *testing.T) {
	rec := httptest.NewRecorder()
	notifyChannelLookupErr(rec, db.ErrNotificationChannelNotFound)
	if rec.Code != 404 {
		t.Fatalf("status=%d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	got := decodeErrorField(t, rec.Body.Bytes())
	if got != notifyErrChannelNotFound {
		t.Fatalf("response=%q, want %q", got, notifyErrChannelNotFound)
	}
	assertEnglishError(t, "channel_not_found", got)
}

// Construct the synthetic channel-test message without database/network access and verify title, classification and summary.
func TestNotifyTestMessageLocalized(t *testing.T) {
	const base = "https://artex.example.test"
	msg := notifyTestMessage(base)
	if len(msg.Items) != 1 {
		t.Fatalf("Test message item count=%d, want 1", len(msg.Items))
	}
	item := msg.Items[0]
	assertEnglishError(t, "test.name", item.Name)
	assertEnglishError(t, "test.class", item.VulnClass)
	assertEnglishError(t, "test.summary", item.Summary)
	if msg.HomeURL != base || item.DetailURL != base {
		t.Fatalf("Links were not preserved: HomeURL=%q DetailURL=%q (want %q)", msg.HomeURL, item.DetailURL, base)
	}
}

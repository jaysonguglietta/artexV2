package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Test English Telegram/webhook/shared HTTP validation and transport messages. ASCII data isolates labels from unchanged target evidence.

// Exercise all single-Telegram label branches and reject untranslated Han framing.
func TestTelegramItemLabelsLocalized(t *testing.T) {
	m := Message{
		HomeURL: "https://example.com/panel",
		Items: []Item{{
			Name:       "sqli-login", // Title() = Name
			VulnClass:  "injection",  // Use a distinct class to render its row.
			Severity:   "high",       // English high-severity label.
			Summary:    "login form is injectable",
			Assets:     []string{"host-a.example.com"},
			DetailURL:  "https://example.com/f/1",
			FromStatus: "pending", // Status-change event enables its transition row.
			ToStatus:   "fixed",
		}},
	}
	out, kept := telegramHTML(m)
	if kept != 1 {
		t.Fatalf("single message should report one item, got %d", kept)
	}
	if hasHan(out) {
		t.Errorf("Telegram body contains Han text:\n%s", out)
	}
	for _, want := range []string{"Status changed", "Type", "Assets", "Overview", "View details"} {
		if !strings.Contains(out, want) {
			t.Errorf("Telegram body lacks %q label:\n%s", want, out)
		}
	}
}

// Test English Telegram digest titles across overflow and time-window branches, consistent with shared titles.
func TestTelegramBatchTitleLocalized(t *testing.T) {
	items := []Item{{Name: "a", Severity: "high"}, {Name: "b", Severity: "low"}}

	// Overflow plus a time window includes kept/remaining counts and the next-message notice.
	over := telegramBatchTitle(Message{WindowMinutes: 30}, items, 5)
	assertEnglish(t, "telegramBatchTitle(overflow)", over)
	for _, want := range []string{"Finding summary - total 5", "showing first 2", "remaining 3", "next message", "Last 30 minutes"} {
		if !strings.Contains(over, want) {
			t.Errorf("overflow title requires %q: %q", want, over)
		}
	}

	// No overflow/window omits continuation wording.
	full := telegramBatchTitle(Message{}, items, 2)
	assertEnglish(t, "telegramBatchTitle(complete)", full)
	if !strings.Contains(full, "Finding summary - total 2") {
		t.Errorf("complete title should contain Finding summary - total 2: %q", full)
	}
	if strings.Contains(full, "next message") {
		t.Errorf("unexpected continuation notice without overflow: %q", full)
	}
}

// Required Telegram errors identify Bot Token or Chat ID in English.
func TestTelegramValidateLocalized(t *testing.T) {
	if err := (telegramChannel{}).Validate(map[string]any{}); err == nil {
		t.Fatal("missing bot_token should fail validation")
	} else {
		assertEnglish(t, "telegram Validate(bot_token)", err.Error())
		if !strings.Contains(err.Error(), "Bot Token") {
			t.Errorf("error should identify missing Bot Token: %q", err.Error())
		}
	}
	if err := (telegramChannel{}).Validate(map[string]any{"bot_token": "t"}); err == nil {
		t.Fatal("missing chat_id should fail validation")
	} else {
		assertEnglish(t, "telegram Validate(chat_id)", err.Error())
		if !strings.Contains(err.Error(), "Chat ID") {
			t.Errorf("error should identify missing Chat ID: %q", err.Error())
		}
	}
}

// Test actionable English generic-webhook validation errors.
func TestWebhookValidateLocalized(t *testing.T) {
	if err := (webhookChannel{}).Validate(map[string]any{}); err == nil {
		t.Fatal("missing URL should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(url)", err.Error())
		if !strings.Contains(err.Error(), "Destination URL") {
			t.Errorf("error should identify missing destination URL: %q", err.Error())
		}
	}
	// Unsupported methods expose the permitted method list.
	if err := (webhookChannel{}).Validate(map[string]any{"url": "https://example.com/hook", "method": "DELETE"}); err == nil {
		t.Fatal("DELETE should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(method)", err.Error())
		if !strings.Contains(err.Error(), "GET, POST, PUT, or PATCH") {
			t.Errorf("error should list permitted methods: %q", err.Error())
		}
	}
	// Template syntax errors retain their standard Go cause within English framing.
	if err := (webhookChannel{}).Validate(map[string]any{"url": "https://example.com/hook", "body_template": "{{"}); err == nil {
		t.Fatal("invalid template should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(template)", err.Error())
		if !strings.Contains(err.Error(), "template") {
			t.Errorf("error should identify template failure: %q", err.Error())
		}
	}
}

// Test the URL helper shared by five webhook-style adapters for English error wording.
func TestValidateHTTPURLLocalized(t *testing.T) {
	// Unsupported scheme.
	if err := validateHTTPURL("ftp://example.com/x"); err == nil {
		t.Fatal("FTP scheme should be rejected")
	} else {
		assertEnglish(t, "validateHTTPURL(scheme)", err.Error())
		if !strings.Contains(err.Error(), "HTTP") {
			t.Errorf("error should list supported HTTP/HTTPS schemes: %q", err.Error())
		}
	}
	// Missing hostname.
	if err := validateHTTPURL("http://"); err == nil {
		t.Fatal("hostless URL should be rejected")
	} else {
		assertEnglish(t, "validateHTTPURL(host)", err.Error())
	}
}

// Local/link-local rejection errors explain the explicit operator exception.
func TestHTTPLocalTargetBlockedLocalized(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "") // Explicitly restore default blocking.
	err := blockInternalDial("tcp", "127.0.0.1:25", nil)
	if err == nil {
		t.Fatal("local address should be blocked by default")
	}
	assertEnglish(t, "blockInternalDial", err.Error())
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("denial should identify explicit opt-in %s: %q", AllowLocalTargetsEnv, err.Error())
	}
}

// Test English redaction placeholders and unknown transport-error handling.
func TestHTTPRedactHelpersLocalized(t *testing.T) {
	if got := redactRequestTarget(""); !strings.Contains(got, "unresolvable") {
		t.Errorf("unparseable address should use an English placeholder, got %q", got)
	}
	// url.Error with a nil underlying cause.
	got := redactTransportError(&url.Error{Op: "Get", URL: "http://api.example", Err: nil})
	if hasHan(got) {
		t.Errorf("redacted transport error contains Han text: %q", got)
	}
	if !strings.Contains(got, "unknown error") {
		t.Errorf("nil underlying cause should return unknown error, got %q", got)
	}
}

// Test real local doJSON status classification with explicitly permitted synthetic receivers.
func TestHTTPStatusErrorsLocalized(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "1")
	cases := []struct {
		code int
		want string
	}{
		{http.StatusForbidden, "rejected request"},
		{http.StatusInternalServerError, "server error"},
		{http.StatusTooManyRequests, "throttled"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte("body"))
		}))
		_, err := doJSON(context.Background(), http.MethodPost, srv.URL, nil, map[string]any{"a": 1})
		srv.Close()
		if err == nil {
			t.Fatalf("HTTP %d should produce an error", tc.code)
		}
		assertEnglish(t, "doJSON(HTTP status)", err.Error())
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("HTTP %d error should contain %q, got %q", tc.code, tc.want, err.Error())
		}
	}
}

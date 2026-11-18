package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Test dial-time loopback/metadata protection and credential-free URL-validation errors. Normal fixtures enable local receivers in TestMain; every SSRF fixture explicitly restores the default deny behavior.

func TestMain(m *testing.M) {
	// Allow synthetic local receivers for ordinary tests; SSRF cases clear this setting.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// Core invariant: the connection layer rejects loopback by default.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // Clear the local-target exception.
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("default policy should deny loopback delivery")
	}
	if hit {
		t.Fatal("local receiver was contacted; dial guard failed")
	}
	// Explain the explicit opt-in for legitimate local relays.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("denial should explain explicit opt-in: %v", err)
	}
}

// Explicit opt-in permits normal local SMTP/webhook deployments.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("explicit local opt-in should deliver: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // Cloud metadata is a primary protected destination.
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // Normalize IPv4-mapped IPv6 before classification.
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be rejected", s)
		}
	}
	// RFC1918 is deliberately allowed for private Mattermost/SMTP services; pin that explicit deployment tradeoff.
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed for private delivery services", s)
		}
	}
}

// Reject forbidden literal IPs at configuration time for immediate feedback.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s should fail configuration validation", raw)
		}
	}
	// Public and RFC1918 addresses pass these checks.
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s should pass validation: %v", raw, err)
		}
	}
}

// Exercise actual url.Parse errors, not merely unsupported schemes that still parse successfully. Parser errors contain the original credential-bearing URL and require independent redaction coverage.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // Invalid percent escape.
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // Nonnumeric port.
		"http://[::1?access_token=" + leakProbeToken,                  // Unbalanced bracket.
	}
	for _, raw := range cases {
		// First prove that the fixture really triggers url.Parse failure rather than another branch.
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q must fail parsing to exercise the intended branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q should fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// Channel wrapping must not reintroduce the address.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("invalid address should fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// SMTP must use the same dial guard; an unguarded socket permits metadata/loopback probing and peer-line disclosure through errors. Explicitly clear the TestMain local exception so the test cannot pass without the guard.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // Clear the local-target exception.
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("default SMTP policy should deny loopback")
	}
	// The Control hook rejects before connection or EHLO.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("SMTP session established despite dial guard")
	}
	// Explain the opt-in for a legitimate local relay.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("denial should explain explicit opt-in: %v", err)
	}
}

// The paired opt-in fixture proves normal local SMTP still works.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("explicit local SMTP opt-in should deliver: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("no EHLO observed; session did not establish")
	}
}

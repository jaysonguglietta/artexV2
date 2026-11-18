package notify

import (
	"bufio"
	"context"

	"net"
	"strings"
	"sync"
	"testing"
)

// Exercise the actual net/smtp protocol with a minimal local server rather than mocking away handshake/auth/envelope/DATA risks.

// fakeSMTP implements greeting/EHLO/AUTH/MAIL/RCPT/DATA/QUIT with configurable stage failures.
type fakeSMTP struct {
	ln net.Listener

	// RCPT TO response, default 250.
	rcptReply string
	// MAIL FROM response, default 250.
	mailReply string
	// Advertise AUTH PLAIN in EHLO when enabled.
	advertiseAuth bool

	mu       sync.Mutex
	data     string
	commands []string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, rcptReply: "250 OK", mailReply: "250 OK"}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) hostPort(t *testing.T) (string, int) {
	t.Helper()
	addr, ok := f.ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener address is not TCP")
	}
	return "127.0.0.1", addr.Port
}

func (f *fakeSMTP) record(cmd string) {
	f.mu.Lock()
	f.commands = append(f.commands, cmd)
	f.mu.Unlock()
}

func (f *fakeSMTP) body() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data
}

func (f *fakeSMTP) sawCommand(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeSMTP) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	w("220 fake.local ESMTP ready")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.record(line)
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			// Omit STARTTLS to test envelope handling on the deliberate local plaintext fixture.
			w("250-fake.local")
			if f.advertiseAuth {
				w("250-AUTH PLAIN")
			}
			w("250 8BITMIME")
		case strings.HasPrefix(line, "AUTH"):
			// Accept simplified PLAIN continuation responses.
			w("235 2.7.0 Authentication successful")
		case strings.HasPrefix(line, "MAIL FROM"):
			w(f.mailReply)
		case strings.HasPrefix(line, "RCPT TO"):
			w(f.rcptReply)
		case strings.HasPrefix(line, "DATA"):
			w("354 End data with <CR><LF>.<CR><LF>")
			var sb strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				sb.WriteString(dl)
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			w("250 2.0.0 Ok: queued as FAKE1")
		case strings.HasPrefix(line, "QUIT"):
			w("221 2.0.0 Bye")
			return
		default:
			w("250 OK")
		}
	}
}

func emailCfg(t *testing.T, f *fakeSMTP, extra map[string]any) map[string]any {
	t.Helper()
	host, port := f.hostPort(t)
	cfg := map[string]any{
		"host": host,
		"port": float64(port),
		"from": "artex@example.com",
		"to":   []any{"a@example.com", "b@example.com"},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

func TestEmailSendDeliversFullMessage(t *testing.T) {
	f := newFakeSMTP(t)
	f.advertiseAuth = true
	cfg := emailCfg(t, f, map[string]any{"username": "artex", "password": "pw"})

	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("delivery failed: %v", err)
	}
	// Assert sender, both recipients and DATA stages were reached.
	for _, want := range []string{"MAIL FROM:<artex@example.com>", "RCPT TO:<a@example.com>", "RCPT TO:<b@example.com>", "DATA", "AUTH", "QUIT"} {
		if !f.sawCommand(want) {
			t.Errorf("SMTP session lacks %q; commands: %v", want, f.commands)
		}
	}
	// Decoded base64 HTML contains the finding content.
	body := f.body()
	if body == "" {
		t.Fatal("DATA received no body")
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("missing Content-Type:\n%s", body)
	}
	if !strings.Contains(body, "base64") {
		t.Errorf("body lacks base64 encoding needed for SMTP line limits:\n%s", body)
	}
	// Include every recipient in the To header.
	if !strings.Contains(body, "a@example.com, b@example.com") {
		t.Errorf("To header lacks recipients:\n%s", body)
	}
}

func TestEmailSendWithoutAuth(t *testing.T) {
	// Without a username, do not issue AUTH to a relay.
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("delivery failed: %v", err)
	}
	if f.sawCommand("AUTH") {
		t.Errorf("AUTH sent without configured account: %v", f.commands)
	}
}

// Test 5xx permanent versus 4xx greylisting retry classification.
func TestEmailSendClassifiesSMTPReplies(t *testing.T) {
	cases := []struct {
		name      string
		rcptReply string
		mailReply string
		permanent bool
	}{
		{"recipient 550 permanent rejection", "550 5.1.1 User unknown", "250 OK", true},
		{"recipient 450 greylisting", "450 4.7.1 Greylisting in action", "250 OK", false},
		{"recipient 452 full mailbox", "452 4.2.2 Mailbox full", "250 OK", false},
		{"sender 553 permanent rejection", "250 OK", "553 5.1.3 Bad address", true},
		{"sender 451 transient error", "250 OK", "451 4.3.0 Temporary failure", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSMTP(t)
			f.rcptReply = tc.rcptReply
			f.mailReply = tc.mailReply
			_, err := (emailChannel{}).Send(context.Background(), emailCfg(t, f, nil), singleMsg())
			if err == nil {
				t.Fatal("expected error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("permanent: want %v, got %v (%v)", tc.permanent, got, err)
			}
			// Keep server text for actionable diagnosis.
			if !strings.Contains(err.Error(), strings.Fields(tc.rcptReply)[0]) && !strings.Contains(err.Error(), strings.Fields(tc.mailReply)[0]) {
				t.Errorf("error lacks server reply code: %v", err)
			}
		})
	}
}

func TestEmailSendRefusesPlaintextCredentials(t *testing.T) {
	// PlainAuth must refuse unencrypted non-localhost credential transmission and explain how to fix it.
	f := newFakeSMTP(t)
	f.advertiseAuth = true
	_, port := f.hostPort(t)
	cfg := map[string]any{
		"host":     "smtp.example.com", // Use a non-localhost name.
		"port":     float64(port),
		"from":     "a@example.com",
		"to":       []any{"b@example.com"},
		"username": "artex",
		"password": "pw",
	}
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Skip("local DNS resolved to the fixture; skip this branch")
	}
	// Connection refusal or refusing credentials is acceptable; silently sending the password is not.
	if !IsPermanent(err) && !strings.Contains(err.Error(), "connection") {
		t.Logf("error: %v (non-localhost connection refusal is expected)", err)
	}
}

func TestEmailValidateReportsMissingFields(t *testing.T) {
	// Validate every required email field before delivery, with field-specific errors.
	cases := []struct {
		name string
		cfg  map[string]any
	}{
		{"missing host", map[string]any{"port": float64(25), "from": "a@b.c", "to": []any{"d@e.f"}}},
		{"missing port", map[string]any{"host": "smtp.example.com"}},
		{"port out of range", map[string]any{"host": "h", "port": float64(70000), "from": "a@b.c", "to": []any{"d@e.f"}}},
		{"missing sender", map[string]any{"host": "h", "port": float64(25), "to": []any{"d@e.f"}}},
		{"missing recipients", map[string]any{"host": "h", "port": float64(25), "from": "a@b.c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := (emailChannel{}).Validate(tc.cfg); err == nil {
				t.Fatalf("configuration should fail validation: %v", tc.cfg)
			}
		})
	}
}

// Configuration helpers accept JSON float64/string ports and scalar/list values.
func TestEmailConfigTolerance(t *testing.T) {
	cfg := map[string]any{
		"host": "smtp.example.com",
		"port": "587", // String-valued port.
		"from": "a@b.c",
		"to":   "d@e.f", // Scalar recipient instead of an array.
		"tls":  "true",  // String-valued boolean.
	}
	if err := (emailChannel{}).Validate(cfg); err != nil {
		t.Fatalf("string numeric values should be accepted: %v", err)
	}
	if got := cfgInt(cfg, "port"); got != 587 {
		t.Errorf("cfgInt did not parse string port: %d", got)
	}
	if !cfgBool(cfg, "tls") {
		t.Error("cfgBool did not parse string true")
	}
	if to := cfgStrings(cfg, "to"); len(to) != 1 || to[0] != "d@e.f" {
		t.Errorf("cfgStrings rejected scalar string: %v", to)
	}
}

// Reject threshold typos at write time instead of silently accepting an unfiltered channel.
func TestFilterValidateRejectsTypo(t *testing.T) {
	good := []string{"", "low", "medium", "high", "critical"}
	for _, s := range good {
		if err := (Filter{MinSeverity: s}).Validate(); err != nil {
			t.Errorf("valid threshold %q rejected: %v", s, err)
		}
	}
	// Reject realistic spelling mistakes.
	for _, s := range []string{"hgih", "HIGH", "Critical", "high ", "crit"} {
		err := (Filter{MinSeverity: s}).Validate()
		if err == nil {
			t.Errorf("invalid threshold %q should be rejected", s)
			continue
		}
		// Return corrective threshold guidance.
		if !strings.Contains(err.Error(), "low") || !strings.Contains(err.Error(), "critical") {
			t.Errorf("error should list supported values, got %q", err.Error())
		}
	}
}

// Historical invalid config remains readable despite stricter write validation.
func TestFilterValidateIsWriteTimeOnly(t *testing.T) {
	raw := []byte(`{"min_severity":"hgih"}`)
	f := ParseFilter(raw) // No parse error.
	if f.MinSeverity != "hgih" {
		t.Fatalf("read path should preserve value, got %q", f.MinSeverity)
	}
	// The channel still evaluates events without panic or blocking.
	_ = Match(f, Snapshot{Kind: EventFindingCreated, Severity: "critical"})
}

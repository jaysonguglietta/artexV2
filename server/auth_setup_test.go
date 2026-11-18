package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Database-free unit coverage for setup-token matching, password-bound session signatures, origin controls, failed-login limits and loopback binding. auth_init_db_test.go covers the complete database flow.

func TestSigningKeyBoundToPasswordHash(t *testing.T) {
	s := &Server{jwtKey: []byte(strings.Repeat("k", 32))}
	if !bytes.Equal(s.signingKey(""), s.jwtKey) {
		t.Fatal("Without a password, retain the file key for database-free fixtures")
	}
	tok, err := signJWT(s.signingKey("hash-A"))
	if err != nil {
		t.Fatal(err)
	}
	if !verifyJWT(tok, s.signingKey("hash-A")) {
		t.Fatal("A key derived from the same hash must verify")
	}
	if verifyJWT(tok, s.signingKey("hash-B")) {
		t.Fatal("Changing the password hash must invalidate old tokens")
	}
	if verifyJWT(tok, s.signingKey("")) {
		t.Fatal("A hash-derived signature must not verify with the raw file key")
	}
}

func TestVerifyTokenInvalidatedByPasswordChange(t *testing.T) {
	s := &Server{jwtKey: []byte(strings.Repeat("v", 32))}
	s.setPassHash("hash-A")
	tok, err := signJWT(s.signingKey("hash-A"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.verifyToken(tok) {
		t.Fatal("A token signed with the current hash must pass")
	}
	s.setPassHash("hash-B")
	if s.verifyToken(tok) {
		t.Fatal("An old token must not pass after password change")
	}
	if s.verifyToken("") {
		t.Fatal("An empty token must not pass")
	}
}

func TestSetupTokenFromEnvAndRandom(t *testing.T) {
	t.Setenv(setupTokenEnv, "fixed-setup-token")
	s := &Server{}
	if !s.checkSetupToken("  fixed-setup-token \n") {
		t.Fatal("Trim surrounding whitespace from an environment setup token")
	}
	if s.checkSetupToken("fixed-setup-tokeN") || s.checkSetupToken("") {
		t.Fatal("Reject different or empty setup tokens")
	}

	t.Setenv(setupTokenEnv, "")
	s = &Server{}
	tok := s.currentSetupToken()
	if len(tok) != setupTokenLen {
		t.Fatalf("Random setup token length=%d, want %d", len(tok), setupTokenLen)
	}
	if s.currentSetupToken() != tok {
		t.Fatal("A generated token must stay stable until cleared")
	}
	if !s.checkSetupToken(tok) || s.checkSetupToken(tok[:len(tok)-1]) {
		t.Fatal("Random setup tokens require exact equality")
	}
	s.clearSetupToken()
	if s.currentSetupToken() == tok {
		t.Fatal("Clearing initialization state must produce a fresh token")
	}
}

func TestAuthLimiterCooldown(t *testing.T) {
	l := newAuthLimiter()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	const key = "203.0.113.9"

	for i := 0; i < authFreeFailures; i++ {
		if !l.allow(key) {
			t.Fatalf("Attempt before failure %d should be allowed", i+1)
		}
		l.fail(key)
	}
	if l.allow(key) {
		t.Fatalf("After %d failures, block during cooldown", authFreeFailures)
	}
	now = now.Add(authCooldownBase + time.Second)
	if !l.allow(key) {
		t.Fatal("Allow again after the initial cooldown")
	}
	l.fail(key)
	now = now.Add(authCooldownBase + time.Second)
	if l.allow(key) {
		t.Fatal("Every additional failure must double cooldown")
	}
	now = now.Add(authCooldownBase)
	if !l.allow(key) {
		t.Fatal("Allow again after the doubled cooldown")
	}
	l.reset(key)
	l.fail(key)
	if !l.allow(key) {
		t.Fatal("A single failure after success resets to the free allowance")
	}
	for i := 0; i < 20; i++ {
		l.fail(key)
	}
	if got := l.clients[key].until.Sub(now); got > authCooldownMax {
		t.Fatalf("Cooldown exceeded its cap: %v > %v", got, authCooldownMax)
	}
	now = now.Add(authFailureWindow + time.Second)
	if !l.allow(key) {
		t.Fatal("Allow after the failure-record window expires")
	}
	if l.allow("198.51.100.1") != true {
		t.Fatal("Other clients must remain unaffected")
	}
}

func TestCORSAllowlist(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := corsWith(map[string]bool{"http://localhost:5173": true}, ok)

	call := func(method, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/auth/init", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	if rec := call(http.MethodPost, ""); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("Requests without Origin must not receive CORS headers")
	}
	rec := call(http.MethodPost, "https://evil.example")
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("Unlisted origins must not receive Access-Control-Allow-Origin")
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatal("Origin-dependent responses require Vary: Origin")
	}
	if rec := call(http.MethodOptions, "https://evil.example"); rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("Unlisted preflight: code=%d ACAO=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	rec = call(http.MethodPost, "http://LOCALHOST:5173")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://LOCALHOST:5173" {
		t.Fatalf("Return the allowed origin with case-insensitive matching: %q", got)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatal("Allowed origins must permit the Authorization header")
	}

	wild := corsWith(map[string]bool{"*": true}, ok)
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Header.Set("Origin", "https://anything.example")
	w := httptest.NewRecorder()
	wild.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "https://anything.example" {
		t.Fatal("Explicit wildcard retains the legacy all-origin behavior")
	}
}

func TestCORSAllowedOriginsEnv(t *testing.T) {
	t.Setenv(corsOriginsEnv, "")
	got := corsAllowedOrigins()
	for _, o := range defaultCORSOrigins {
		if !got[o] {
			t.Fatalf("Default allowlist lacks %s", o)
		}
	}
	t.Setenv(corsOriginsEnv, " https://UI.example.com , http://10.0.0.2:5173,, ")
	got = corsAllowedOrigins()
	if !got["https://ui.example.com"] || !got["http://10.0.0.2:5173"] || len(got) != 2 {
		t.Fatalf("Unexpected environment allowlist parsing: %v", got)
	}
	if got["http://localhost:5173"] {
		t.Fatal("An explicit environment allowlist replaces defaults")
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		":8787":           false,
		"0.0.0.0:8787":    false,
		"[::]:8787":       false,
		"10.0.0.5:8787":   false,
		"127.0.0.1:8787":  true,
		"127.0.0.1":       true,
		"localhost:8787":  true,
		"[::1]:8787":      true,
		"192.168.0.10:80": false,
	}
	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q)=%v, want %v", addr, got, want)
		}
	}
}

func TestClientKeyIgnoresForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = "198.51.100.7:40000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := clientKey(r); got != "198.51.100.7" {
		t.Fatalf("Throttle by TCP peer, not spoofable X-Forwarded-For: %q", got)
	}
	if id := clientLogID(r); !strings.Contains(id, "198.51.100.7:40000") || !strings.Contains(id, "1.2.3.4") {
		t.Fatalf("Log identity must include both values: %q", id)
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionCookieAndQueryRejection(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "https://app.example/api/auth/login", nil)
	setSessionCookie(w, r, "canary")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie is not protected")
	}
	if extractToken(httptest.NewRequest("GET", "/api/stats?token=secret", nil)) != "" {
		t.Fatal("query bearer accepted")
	}
	a, _ := signJWT([]byte(strings.Repeat("k", 32)))
	b, _ := signJWT([]byte(strings.Repeat("k", 32)))
	if a == b {
		t.Fatal("sessions must have unique token IDs")
	}
}

func TestLogoutRevokesSessionDurablyDB(t *testing.T) {
	rig := newAuthTestRig(t)
	rig.s.setPassHash("")
	tok, err := signJWT(rig.s.signingKey(""))
	if err != nil {
		t.Fatal(err)
	}
	if !rig.s.verifyToken(tok) {
		t.Fatal("valid session rejected")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://localhost:8787/api/auth/logout", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rig.s.authLogout(w, r)
	if w.Code != 200 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	// A new server instance reads the same revocation, rather than trusting a process cache.
	other := &Server{m: rig.s.m, jwtKey: rig.s.jwtKey}
	other.setPassHash("")
	if other.verifyToken(tok) {
		t.Fatal("logged-out session survived new server instance")
	}
}

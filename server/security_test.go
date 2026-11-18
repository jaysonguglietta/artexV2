package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSecurityBoundaryBodyAndOrigin(t *testing.T) {
	reached := false
	h := securityBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; w.WriteHeader(204) }))
	for _, tt := range []struct {
		body, origin string
		status       int
	}{
		{strings.Repeat("x", 8193), "", 413}, {"{}", "https://evil.example", 403}, {"{}", "http://example.com", 204},
	} {
		reached = false
		r := httptest.NewRequest("POST", "http://example.com/api/auth/login", strings.NewReader(tt.body))
		r.Header.Set("Origin", tt.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("status=%d want %d", w.Code, tt.status)
		}
		if tt.status != 204 && reached {
			t.Fatal("rejected request reached auth handler")
		}
	}
}

func TestAuthenticationConcurrencyBound(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	h := securityBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release }))
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/auth/login", nil))
		}()
	}
	for range 4 {
		<-entered
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/login", nil))
	close(release)
	wg.Wait()
	if w.Code != 429 {
		t.Fatalf("concurrent request status=%d", w.Code)
	}
}

func TestDevelopmentOriginExceptionIsExplicitAndLocal(t *testing.T) {
	for _, tc := range []struct {
		configured, origin, host string
		want                     int
	}{
		{"", "http://127.0.0.1:3000", "127.0.0.1:8787", 403},
		{"http://127.0.0.1:3000", "http://127.0.0.1:3000", "127.0.0.1:8787", 204},
		{"http://127.0.0.1:3000", "http://evil.example", "127.0.0.1:8787", 403},
		{"http://evil.example", "http://evil.example", "127.0.0.1:8787", 403},
		{"http://127.0.0.1:3000", "http://127.0.0.1:3000", "example.com:8787", 403},
	} {
		t.Run(tc.configured+tc.origin+tc.host, func(t *testing.T) {
			t.Setenv("ARTEX_DEV_ORIGIN", tc.configured)
			h := securityBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/test", nil)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}

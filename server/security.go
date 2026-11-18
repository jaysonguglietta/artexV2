package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// securityBoundary applies limits before decoding, authentication and bcrypt.
// A small global admission budget bounds expensive authentication work, including
// simultaneous requests from one peer that have not yet recorded a failure.
func securityBoundary(next http.Handler) http.Handler {
	authSlots := make(chan struct{}, 4)
	// Next's development proxy changes Host. This explicit local-only exception
	// is set by dev.sh; never trust forwarded headers or arbitrary origins.
	devOrigin := os.Getenv("ARTEX_DEV_ORIGIN")
	if devOrigin != "http://127.0.0.1:3000" && devOrigin != "http://localhost:3000" {
		devOrigin = ""
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				localDev := devOrigin != "" && origin == devOrigin && (r.Host == "127.0.0.1:8787" || r.Host == "localhost:8787")
				if err != nil || (u.Host != r.Host && !localDev) || (u.Scheme != "http" && u.Scheme != "https") {
					writeErr(w, http.StatusForbidden, "Cross-origin mutations are not permitted")
					return
				}
			} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeErr(w, http.StatusForbidden, "Cross-site mutations are not permitted")
				return
			}
		}
		limit := int64(32 << 20)
		if strings.Contains(r.URL.Path, "upload") {
			limit = 512 << 20
		}
		authWork := r.Method == http.MethodPost && (r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/init" || r.URL.Path == "/api/auth/change-password")
		if authWork {
			limit = 8 << 10
			select {
			case authSlots <- struct{}{}:
				defer func() { <-authSlots }()
			default:
				w.Header().Set("Retry-After", "5")
				writeErr(w, 429, "Authentication is busy; retry shortly")
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			if r.ContentLength > limit {
				writeErr(w, 413, "Request body exceeds the size limit")
				return
			}
			// Stream responses remain unlimited; only reading a request body has a deadline.
			if r.Method != http.MethodGet {
				_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Second))
			}
		}
		next.ServeHTTP(w, r)
	})
}

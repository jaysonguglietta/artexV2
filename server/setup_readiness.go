package server

import (
	"context"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/isolation"
)

// Authenticated and read-only. Never resolves targets or changes broker policy.
func (s *Server) setupReadiness(w http.ResponseWriter, r *http.Request) {
	session := ""
	if raw := r.URL.Query().Get("task"); raw != "" {
		id, valid := canonicalTaskID(raw)
		if !valid {
			writeErr(w, 400, "Invalid assessment ID")
			return
		}
		if _, ok := s.m.Task(id); !ok {
			writeErr(w, 404, "Assessment not found")
			return
		}
		session = "task-" + id
	}
	state := "not_configured"
	approved := false
	if isolation.Configured() {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var err error
		approved, err = isolation.CheckPolicy(ctx, session)
		if err != nil {
			state = "unavailable"
		} else {
			state = "connected"
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"worker": state, "target_approved": approved})
}

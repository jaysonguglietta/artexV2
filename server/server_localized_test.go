package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// English server-response coverage includes direct HTTP errors, profile validation, chat availability/test errors, fallback confirmations/status and legacy command aliases. Raw user inputs and protocol identifiers remain separate from narrative.

// Check every response constant, including formatted representative values.
func TestServerErrorConstantsLocalized(t *testing.T) {
	plain := map[string]string{
		"intent_control":        errTaskDeletingIntentControl,
		"intent_rerun":          errTaskDeletingIntentRerun,
		"intent_not_rerunnable": errIntentNotRerunnable,
		"source_invalid":        errCreateTaskSourceInvalid,
		"intercept_rules":       errCreateTaskInterceptRules,
		"category_invalid":      errCreateTaskCategoryInvalid,
		"company_invalid":       errCreateTaskCompanyInvalid,
		"llm_profile_invalid":   errLLMProfileInvalid,
		"asset_store_disabled":  errAssetStoreDisabled,
		"asset_id_required":     errAssetIDRequired,
		"python_not_detected":   errPythonNotDetected,
		"notify_base_url":       errNotifyBaseURLScheme,
		"notify_digest":         errNotifyDigestRange,
		"new_session":           errTaskDeletingNewSession,
		"new_message":           errTaskDeletingNewMessage,
		"main_agent_busy":       errMainAgentBusy,
		// User-facing responses outside writeErr.
		"chat_no_profile":       errChatNoLLMProfile,
		"chat_no_active":        errChatNoActiveLLMProfile,
		"chat_not_ready":        errChatLLMNotReady,
		"llm_test_no_api_key":   errLLMTestNoAPIKey,
		"web_search_no_results": errWebSearchProbeNoResults,
		// Fallback intent/hint confirmations append user-supplied text.
		"fallback_intent": fallbackIntentInjected,
		"fallback_hint":   fallbackHintRecorded,
	}
	for label, msg := range plain {
		assertEnglishError(t, label, msg)
	}

	formatted := map[string]string{
		"source_limit":     fmt.Sprintf(errCreateTaskSourceLimit, 8),
		"source_not_found": fmt.Sprintf(errCreateTaskSourceNotFound, 999),
		"company_limit":    fmt.Sprintf(errCreateTaskCompanyLimit, 32),
		"llm_profile_404":  fmt.Sprintf(errLLMProfileNotFound, 7),
		// Fallback status substitutes asset, queued-intent and finding counts.
		"fallback_status": fmt.Sprintf(fallbackChatStatus, 3, 2, 1),
	}
	for label, msg := range formatted {
		assertEnglishError(t, label, msg)
	}
}

// Exercise actual database-free handler bodies to prove constants are wired into responses. Constants cover later database/engine-dependent paths.
func TestServerHandlersResponsesLocalized(t *testing.T) {
	// The deletion barrier marks the task as deleting.
	deletingServer := func() *Server {
		s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
		s.engine.deleting.Store("t1", true)
		return s
	}
	// Without a deletion barrier, handlers reach request validation.
	liveServer := func() *Server {
		return &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
	}

	cases := []struct {
		name    string
		req     func() *http.Request
		handler func(*Server) http.HandlerFunc
		server  func() *Server
		code    int
		want    string
	}{
		{
			name: "controlIntent/task-deleting",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/5/control", strings.NewReader(`{"action":"pause"}`))
				r.SetPathValue("id", "t1")
				r.SetPathValue("iid", "5")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.controlIntent },
			server:  deletingServer,
			code:    http.StatusConflict,
			want:    errTaskDeletingIntentControl,
		},
		{
			name: "rerunIntent/task-deleting",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/5/rerun", nil)
				r.SetPathValue("id", "t1")
				r.SetPathValue("iid", "5")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.rerunIntent },
			server:  deletingServer,
			code:    http.StatusConflict,
			want:    errTaskDeletingIntentRerun,
		},
		{
			name: "rerunBlocked/task-deleting",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/rerun-blocked", nil)
				r.SetPathValue("id", "t1")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.rerunBlocked },
			server:  deletingServer,
			code:    http.StatusConflict,
			want:    errTaskDeletingIntentRerun,
		},
		{
			name: "createTask/source-limit",
			req: func() *http.Request {
				// MaxTaskSourceCount is eight; nine returns before source iteration.
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"Goal","source_task_ids":["1","2","3","4","5","6","7","8","9"]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    fmt.Sprintf(errCreateTaskSourceLimit, 8),
		},
		{
			name: "createTask/source-invalid",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"Goal","source_task_ids":["abc"]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    errCreateTaskSourceInvalid,
		},
		{
			name: "createTask/source-not-found",
			req: func() *http.Request {
				// Task 999 is absent from the in-memory map; no database lookup is needed.
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"Goal","source_task_ids":["999"]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    fmt.Sprintf(errCreateTaskSourceNotFound, 999),
		},
		{
			name: "createTask/company-limit",
			req: func() *http.Request {
				// MaxTaskCompanyCount is 32; 33 fails pure normalization.
				ids := make([]string, 33)
				for i := range ids {
					ids[i] = fmt.Sprintf("%d", i+1)
				}
				body := `{"goal":"Goal","company_ids":[` + strings.Join(ids, ",") + `]}`
				return httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    fmt.Sprintf(errCreateTaskCompanyLimit, 32),
		},
		{
			name: "createTask/llm-profile-invalid",
			req: func() *http.Request {
				// A zero profile ID fails validation before loading a profile from PostgreSQL.
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"Goal","llm_profile_ids":[0]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    errLLMProfileInvalid,
		},
		{
			name: "taskCoverageGraph/asset-store-disabled",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/tasks/t1/coverage-graph", nil)
				r.SetPathValue("id", "t1")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.taskCoverageGraph },
			server:  liveServer, // Manager.assets==nil → Assets()==nil → 503
			code:    http.StatusServiceUnavailable,
			want:    errAssetStoreDisabled,
		},
		{
			name: "taskAssetRefs/asset-id-required",
			req: func() *http.Request {
				// Missing asset_id parses to zero and fails before Assets access.
				r := httptest.NewRequest(http.MethodGet, "/api/tasks/t1/asset-refs", nil)
				r.SetPathValue("id", "t1")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.taskAssetRefs },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    errAssetIDRequired,
		},
		{
			// An empty profile ID/key fails before TestConnection with ok=false in a 200 response; the fixture also has no global fallback key.
			name: "testLLM/no-api-key",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/api/llm/test", strings.NewReader(`{}`))
			},
			handler: func(s *Server) http.HandlerFunc { return s.testLLM },
			server:  liveServer,
			code:    http.StatusOK,
			want:    errLLMTestNoAPIKey,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.server()
			rec := httptest.NewRecorder()
			c.handler(s)(rec, c.req())
			if rec.Code != c.code {
				t.Fatalf("status=%d, want %d (body %q)", rec.Code, c.code, rec.Body.String())
			}
			got := decodeErrorField(t, rec.Body.Bytes())
			if got != c.want {
				t.Fatalf("response=%q, want %q", got, c.want)
			}
			assertEnglishError(t, c.name, got)
		})
	}
}

// Verify chatUnavailableReason returns the English unready sentinel when PostgreSQL is absent. Pin the other database-dependent reasons through constants.
func TestChatUnavailableReasonLocalized(t *testing.T) {
	s := &Server{m: &Manager{}} // A nil database selects the third return path.
	got := s.chatUnavailableReason()
	if got != errChatLLMNotReady {
		t.Fatalf("chatUnavailableReason()=%q, want %q", got, errChatLLMNotReady)
	}
	assertEnglishError(t, "chat_not_ready", got)
}

// Legacy Korean/Chinese and English command aliases must preserve their parsed arguments. These are compatibility inputs, not presentation language.
func TestFallbackCommandParsing(t *testing.T) {
	cases := []struct {
		name, in, cmd, text string
	}{
		{"english-intent", "intent scan ports", "intent", "scan ports"},
		{"english-hint", "hint try admin:admin", "hint", "try admin:admin"},
		{"status-empty", "   ", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, text := fallbackCommand(c.in)
			if cmd != c.cmd || text != c.text {
				t.Fatalf("fallbackCommand(%q)=(%q, %q), want (%q, %q)", c.in, cmd, text, c.cmd, c.text)
			}
		})
	}
}

package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Test HTTP-layer classification independently of adapter application codes: retry 503, but do not repeatedly retry 403.

func replyServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoJSONClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
	}{
		{"200 success", 200, false},
		{"429 retryable rate limit", 429, false},
		{"408 retryable timeout", 408, false},
		{"500 retryable server failure", 500, false},
		{"502 retryable gateway failure", 502, false},
		{"503 retryable unavailable service", 503, false},
		{"400 permanent parameter error", 400, true},
		{"401 permanent authentication error", 401, true},
		{"403 permanent authorization error", 403, true},
		{"404 permanent missing endpoint", 404, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := replyServer(t, tc.status, `{"detail":"upstream says no"}`)
			_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
			if tc.status < 300 {
				if err != nil {
					t.Fatalf("2xx should succeed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("non-2xx should fail")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("HTTP %d permanent: want %v, got %v (%v)",
					tc.status, tc.permanent, got, err)
			}
			// Assert language-independent numeric status codes in errors for stable diagnostics.
			if !strings.Contains(err.Error(), strconv.Itoa(tc.status)) {
				t.Errorf("error should include HTTP %d, got %v", tc.status, err)
			}
		})
	}
}

// Include the receiver's explanation in a bounded error snippet.
func TestDoJSONIncludesResponseSnippet(t *testing.T) {
	srv := replyServer(t, 400, `{"error":"invalid webhook token"}`)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid webhook token") {
		t.Errorf("error lacks peer explanation: %v", err)
	}
}

// Keep snippets short and single-line because they are stored and rendered verbatim in history.
func TestDoJSONSnippetIsSingleLineAndBounded(t *testing.T) {
	// Use newline/tab and 5000-character response content.
	long := strings.Repeat("x", 5000)
	srv := replyServer(t, 500, "line1\nline2\r\n\tline3 "+long)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\r\n\t") {
		t.Errorf("error should be single-line, got %q", msg)
	}
	// The 200-character snippet plus framing stays far below the original response size.
	if len(msg) > 400 {
		t.Errorf("error too long (%d bytes), expected bounded snippet: %q", len(msg), msg)
	}
}

// Reject oversized responses instead of reading/storing arbitrary amounts of peer data.
func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	huge := strings.Repeat("A", 1<<20) // 1 MiB
	srv := replyServer(t, 400, huge)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > 400 {
		t.Errorf("oversized response was not bounded; error length %d", len(err.Error()))
	}
}

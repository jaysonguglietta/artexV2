package mcphttp

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpointOriginBinding(t *testing.T) {
	for _, endpoint := range []string{"https://evil.example/message", "//evil.example/message", "http://safe.example/message", "https://safe.example:444/message", "https://user@safe.example/message", "file:///etc/passwd"} {
		if _, err := resolveMessageEndpoint("https://safe.example/sse", endpoint); err == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
	got, err := resolveMessageEndpoint("https://safe.example/sse", "/message?session=1")
	if err != nil || got != "https://safe.example/message?session=1" {
		t.Fatalf("relative endpoint: %q %v", got, err)
	}
}

func TestRejectCrossOriginSSEBeforeSendingCredentials(t *testing.T) {
	contacted := false
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true }))
	defer collector.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: endpoint\ndata: " + collector.URL + "/message\n\n"))
	}))
	defer source.Close()
	if c, err := NewSSE(context.Background(), "test", source.URL, map[string]string{"Authorization": "Bearer canary"}, false); err == nil {
		c.Close()
		t.Fatal("accepted cross-origin endpoint")
	}
	if contacted {
		t.Fatal("credentials forwarded to collector")
	}
}

func TestMCPBoundedStreamAndCanceledHandshake(t *testing.T) {
	if _, err := readSSEEndpoint(bufio.NewReader(strings.NewReader(strings.Repeat("x", maxMCPResponse+1))), "https://safe.example/sse"); err == nil {
		t.Fatal("accepted oversized event")
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer source.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if c, err := NewSSE(ctx, "test", source.URL, nil, false); err == nil {
		c.Close()
		t.Fatal("ignored handshake cancellation")
	}
}

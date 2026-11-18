package isolation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func trustedFixtureServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})}
	oldCall, oldClose, oldLease := brokerHTTPClient, brokerCloseClient, brokerLeaseClient
	brokerHTTPClient, brokerCloseClient, brokerLeaseClient = client, client, client
	t.Cleanup(func() { brokerHTTPClient, brokerCloseClient, brokerLeaseClient = oldCall, oldClose, oldLease })
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARTEX_WORKER_URL", "https://broker.fixture")
	t.Setenv("ARTEX_WORKER_TOKEN_FILE", file)
}
func TestExecuteWithoutRunInfoOwnsOneShotLifecycle(t *testing.T) {
	var calls []Request
	var closed []map[string]string
	var mu sync.Mutex
	trustedFixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/v1/close" {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			closed = append(closed, req)
		} else {
			var req Request
			_ = json.NewDecoder(r.Body).Decode(&req)
			calls = append(calls, req)
		}
		_ = json.NewEncoder(w).Encode(Response{})
	})
	dir := filepath.Join(t.TempDir(), "tool-tests")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), 0, "Custom", json.RawMessage(`{}`), &actool.ToolContext{WorkingDir: dir}, &Custom{Kind: "command"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || !validRunID(calls[0].RunID) || len(closed) != 1 || closed[0]["run_id"] != calls[0].RunID || closed[0]["session"] != calls[0].Session || len(closed[0]) != 2 {
		t.Fatal("one-shot cleanup did not match admitted execution", calls, closed)
	}
}
func TestBorrowedMCPClientCloseDoesNotRetireSharedRun(t *testing.T) {
	var mu sync.Mutex
	closed := map[string]int{}
	trustedFixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/close" {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			closed[req["session"]]++
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(Response{Tools: []Descriptor{}})
	})
	ctx := WithRunID(context.Background(), NewRunID())
	client := &RemoteMCP{}
	if _, err := client.Tools(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := len(closed)
	mu.Unlock()
	if count != 0 {
		t.Fatal("closing one borrowed MCP client killed its entire agent")
	}
	if err := RegisterRunScope(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if err := CloseRunContext(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if closed["discovery"] != 1 || closed["task-1"] != 1 {
		t.Fatal("run cleanup missed MCP/workspace scopes", closed)
	}
	if RegisterRunScope(ctx, "task-2") == nil {
		t.Fatal("closed execution accepted a new scope")
	}
}
func TestBatchMCPClientOwnsAndClosesEphemeralRun(t *testing.T) {
	var mu sync.Mutex
	var ids []string
	var closed []string
	trustedFixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		if r.URL.Path == "/v1/close" {
			closed = append(closed, req.RunID)
		} else {
			ids = append(ids, req.RunID)
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(Response{Result: actool.Text(`{"ok":true}`)})
	})
	client := &RemoteMCP{}
	for i := 0; i < 2; i++ {
		if _, err := client.Call(context.Background(), "batch", map[string]string{"value": "safe"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || !validRunID(ids[0]) || ids[0] != ids[1] || len(closed) != 1 || closed[0] != ids[0] {
		t.Fatal("batch run was not reused/retired exactly once", ids, closed)
	}
}

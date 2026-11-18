package agent

import (
	"context"
	"github.com/Autumn-27/artex/isolation"
	"github.com/Autumn-27/norma/agentcore"
	actool "github.com/Autumn-27/norma/tool"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSDKLateInjectionCannotRestoreLocalExecution(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_HOST_EXECUTION", "")
	t.Setenv("ARTEX_WORKER_URL", "")
	t.Setenv("ARTEX_WORKER_TOKEN_FILE", "")
	dir := filepath.Join(t.TempDir(), "sessions", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	opts := isolatedOptions(context.Background(), agentcore.Options{Tools: []actool.CoreTool{actool.NewSleep()}, EnableWebFetch: true})
	if opts.EnableWebFetch || !opts.DisableBackgroundTasks {
		t.Fatal("SDK can still inject local executors")
	}
	seen := map[string]bool{}
	for _, tool := range opts.Tools {
		if tool.Name() != "Monitor" && tool.Name() != "WebFetch" {
			continue
		}
		seen[tool.Name()] = true
		result, err := tool.Call(context.Background(), []byte(`{"command":"touch must-not-run","url":"http://127.0.0.1"}`), &actool.ToolContext{WorkingDir: dir})
		if err != nil || !result.IsError {
			t.Fatal("missing broker did not refuse executable tool", tool.Name(), result, err)
		}
	}
	if !seen["Monitor"] || !seen["WebFetch"] {
		t.Fatal("late tools were lost instead of isolated")
	}
	if _, err := os.Stat(filepath.Join(dir, "must-not-run")); !os.IsNotExist(err) {
		t.Fatal("local command ran")
	}
	empty := isolatedOptions(context.Background(), agentcore.Options{EnableWebFetch: true, EnableWebSearch: true})
	if empty.Tools == nil || len(empty.Tools) != 0 || empty.EnableWebFetch || empty.EnableWebSearch || !empty.DisableBackgroundTasks {
		t.Fatal("empty policy restored SDK tools")
	}
}

func TestIsolatedSessionCloseRetiresRunExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	session := &isolatedAgentSession{closeRun: func() error { calls.Add(1); return nil }}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := session.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("remote run closed multiple times", calls.Load())
	}
}
func TestIsolatedSessionCancellationStopsRunBeforeLocalCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	session := &isolatedAgentSession{closeRun: func() error { close(done); return nil }}
	session.stop = context.AfterFunc(ctx, func() { session.finishClose() })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled run did not retire remote processes")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestRunInfoAssignsDistinctTrustedExecutionIdentities(t *testing.T) {
	first := WithRunInfo(context.Background(), RunInfo{TaskID: 1})
	second := WithRunInfo(context.Background(), RunInfo{TaskID: 1})
	if RunInfoFrom(first).ExecutionID == "" || RunInfoFrom(first).ExecutionID == RunInfoFrom(second).ExecutionID {
		t.Fatal("sibling runs share execution identity")
	}
	if isolation.RunIDFrom(first) != RunInfoFrom(first).ExecutionID {
		t.Fatal("execution identity is not available to tool assembly")
	}
}

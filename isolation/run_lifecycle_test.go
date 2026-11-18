package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureBroker(t *testing.T) *Broker {
	t.Helper()
	b, err := NewBroker(Policy{Sessions: map[string][]string{"task-1": {"198.18.0.10/32"}}, DNS: []string{"1.1.1.1"}, Management: []string{"192.168.0.0/16"}}, "sha256:"+strings.Repeat("a", 64), "fixture", strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func brokerRequest(b *Broker, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+b.Token)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
func closeBody(id string) map[string]string {
	return map[string]string{"session": "task-1", "run_id": id}
}

type fakeDocker struct {
	mu      sync.Mutex
	running map[string]bool
	created int
	entered chan struct{}
}

func (f *fakeDocker) call(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("missing Docker arguments")
	}
	switch args[0] {
	case "network":
		if args[1] == "ls" {
			return []byte("network-id"), nil
		}
		return []byte(`[{"Name":"fixture","Driver":"bridge","IPAM":{"Config":[{"Subnet":"172.29.0.0/16"}]}}]`), nil
	case "create":
		f.mu.Lock()
		f.created++
		f.mu.Unlock()
		return []byte("container-id"), nil
	case "start":
		f.mu.Lock()
		f.running[args[1]] = true
		f.mu.Unlock()
		return nil, nil
	case "rm":
		f.mu.Lock()
		f.running[args[len(args)-1]] = false
		f.mu.Unlock()
		return nil, nil
	case "exec":
		if args[len(args)-1] == "renew" {
			return nil, nil
		}
		var req Request
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
		if req.Tool == "Block" {
			close(f.entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		response := Response{}
		if req.Tool == "__snapshot" {
			response.SnapshotComplete = true
			response.Files = map[string][]byte{"evidence.txt": []byte("real tmpfs evidence")}
		}
		return json.Marshal(response)
	case "pause", "unpause":
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected Docker command %v", args)
}
func newFake() *fakeDocker {
	return &fakeDocker{running: map[string]bool{}, entered: make(chan struct{})}
}
func TestUrgentCloseCancelsBusyRunWithoutKillingSibling(t *testing.T) {
	b := fixtureBroker(t)
	f := newFake()
	b.dockerFn = f.call
	first, second := strings.Repeat("1", 32), strings.Repeat("2", 32)
	for _, id := range []string{first, second} {
		if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Ready"}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	done := make(chan int, 1)
	go func() {
		done <- brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: first, Tool: "Block"}).Code
	}()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("foreground call did not start")
	}
	started := time.Now()
	if w := brokerRequest(b, "/v1/close", closeBody(first)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if time.Since(started) > time.Second {
		t.Fatal("close queued behind foreground execution")
	}
	select {
	case status := <-done:
		if status != 502 {
			t.Fatal("cancelled request unexpectedly succeeded", status)
		}
	case <-time.After(time.Second):
		t.Fatal("foreground call ignored close cancellation")
	}
	f.mu.Lock()
	aliveFirst, aliveSecond := f.running[b.runName("task-1", first)], f.running[b.runName("task-1", second)]
	created := f.created
	f.mu.Unlock()
	if aliveFirst || !aliveSecond {
		t.Fatal("run cleanup crossed a sibling boundary", aliveFirst, aliveSecond)
	}
	if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: first, Tool: "Ready"}); w.Code != 409 {
		t.Fatal("closed run resurrected", w.Code)
	}
	if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: second, Tool: "Ready"}); w.Code != 200 {
		t.Fatal("sibling was closed", w.Code, w.Body.String())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.created != created {
		t.Fatal("a closed identity created another container")
	}
}
func TestCloseBeforeFirstCallPermanentlyRejectsIdentity(t *testing.T) {
	b := fixtureBroker(t)
	b.dockerFn = func(context.Context, []byte, ...string) ([]byte, error) {
		t.Fatal("Docker was reached for a closed identity")
		return nil, nil
	}
	id := strings.Repeat("c", 32)
	if w := brokerRequest(b, "/v1/close", closeBody(id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Bash"}); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, id := range []string{"", strings.Repeat("A", 32), "../../escape"} {
		if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Bash"}); w.Code != 400 {
			t.Fatal("invalid run identity accepted", id, w.Code)
		}
	}
	if w := brokerRequest(b, "/v1/close", map[string]string{"session": "task-1", "run_id": id, "tool": "Bash"}); w.Code != 400 {
		t.Fatal("close accepted executable payload", w.Code)
	}
}
func TestRunLedgerSurvivesRestartAndRefusesCorruption(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b := fixtureBroker(t)
	b.dockerFn = newFake().call
	if err := b.openLedger(dir); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("d", 32)
	run, fresh, err := b.admit("task-1", id)
	if err != nil || !fresh {
		t.Fatal(err, fresh)
	}
	close(run.ready)
	// Even an active identity becomes closed after broker restart.
	b.Close()
	next := fixtureBroker(t)
	next.dockerFn = newFake().call
	if err = next.openLedger(dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err = next.admit("task-1", id); err == nil {
		t.Fatal("old active identity resurrected after restart")
	}
	next.Close()
	name := filepath.Join(dir, "runs-"+b.owner()+".log")
	if err = os.WriteFile(name, []byte("task-1/incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = fixtureBroker(t).openLedger(dir); err == nil {
		t.Fatal("partial ledger was silently ignored")
	}
}
func TestLedgerSingleWriterAndExhaustionFailClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b := fixtureBroker(t)
	if err := b.openLedger(dir); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := fixtureBroker(t).openLedger(dir); err == nil {
		t.Fatal("two brokers acquired the same ledger")
	}
	for i := 0; i < maxRunLedger; i++ {
		b.known[fmt.Sprintf("task-1/%032x", i)] = true
	}
	if _, _, err := b.admit("task-1", strings.Repeat("e", 32)); err == nil {
		t.Fatal("exhausted ledger admitted new run")
	}
	before := len(b.closed)
	if err := b.closeRun("task-1", strings.Repeat("f", 32)); err == nil {
		t.Fatal("exhausted ledger accepted unrecorded close")
	}
	if len(b.closed) != before {
		t.Fatal("exhausted close identities grew memory without bound")
	}
}
func TestLifecycleUsesRuntimeSnapshotAndRetiresAllSiblingRuns(t *testing.T) {
	b := fixtureBroker(t)
	f := newFake()
	b.dockerFn = f.call
	for _, id := range []string{strings.Repeat("1", 32), strings.Repeat("2", 32)} {
		if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Ready"}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := brokerRequest(b, "/v1/call", Request{Session: "task-1", Tool: "__snapshot"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result Response
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.SnapshotComplete || string(result.Files["evidence.txt"]) != "real tmpfs evidence" {
		t.Fatal("runtime snapshot was not preserved", w.Body.String())
	}
	if w = brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: strings.Repeat("3", 32), Tool: "Ready"}); w.Code != 409 {
		t.Fatal("frozen task admitted run", w.Code)
	}
	if w = brokerRequest(b, "/v1/call", Request{Session: "task-1", Tool: "__retire"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b.mu.Lock()
	remaining := len(b.runs)
	b.mu.Unlock()
	if remaining != 0 {
		t.Fatal("task retirement left siblings", remaining)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, alive := range f.running {
		if alive {
			t.Fatal("retired sibling still runs", name)
		}
	}
}

func TestLeaseCannotCreateOrResurrectAndBypassesScanGate(t *testing.T) {
	b := fixtureBroker(t)
	f := newFake()
	var mu sync.Mutex
	renewals := 0
	b.dockerFn = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "renew" {
			expected := []string{"exec", "--user", "0:0", "--workdir", "/", args[5], "/usr/bin/setpriv", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs", "/usr/local/bin/tool-worker", "renew"}
			if strings.Join(args, " ") != strings.Join(expected, " ") {
				t.Error("lease helper authority expanded", args)
			}
			mu.Lock()
			renewals++
			mu.Unlock()
		}
		return f.call(ctx, input, args...)
	}
	id := strings.Repeat("7", 32)
	if w := brokerRequest(b, "/v1/lease", closeBody(id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(b.known) != 0 || len(b.runs) != 0 {
		t.Fatal("lease created a new run")
	}
	if w := brokerRequest(b, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Ready"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b.gate <- struct{}{} // emulate a long-running scan holding normal admission
	if w := brokerRequest(b, "/v1/lease", closeBody(id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	<-b.gate
	if w := brokerRequest(b, "/v1/close", closeBody(id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := brokerRequest(b, "/v1/lease", closeBody(id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if renewals != 2 {
		t.Fatal("lease renewed a closed/missing run", renewals)
	}
}
func TestCloseAdmissionRemainsAvailableWhenLeaseAndScanSlotsFull(t *testing.T) {
	b := fixtureBroker(t)
	for i := 0; i < cap(b.slots); i++ {
		b.slots <- struct{}{}
	}
	for i := 0; i < cap(b.leaseSlots); i++ {
		b.leaseSlots <- struct{}{}
	}
	if w := brokerRequest(b, "/v1/close", closeBody(strings.Repeat("8", 32))); w.Code != 200 {
		t.Fatal("close was starved by data/lease pools", w.Code, w.Body.String())
	}
}

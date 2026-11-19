package isolation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFailedWorkerCleanupRetainsRunAndRepeatedCloseRetries(t *testing.T) {
	broker := fixtureBroker(t)
	fixture := newFake()
	blocked := false
	removals := 0
	broker.dockerFn = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "rm" && blocked {
			removals++
			return nil, errors.New("synthetic Docker removal unavailable")
		}
		return fixture.call(ctx, input, args...)
	}
	defer broker.Close()
	id := strings.Repeat("d", 32)
	if response := brokerRequest(broker, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Ready"}); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	blocked = true
	for attempt := 1; attempt <= 2; attempt++ {
		response := brokerRequest(broker, "/v1/close", closeBody(id))
		if response.Code != 503 {
			t.Fatalf("uncertain cleanup reported success: attempt=%d status=%d", attempt, response.Code)
		}
		broker.mu.Lock()
		retained := broker.runs[runKeyName("task-1", id)] != nil
		closed := broker.closed[runKeyName("task-1", id)]
		broker.mu.Unlock()
		if !retained || !closed {
			t.Fatal("failed cleanup lost its retry reference or closed tombstone")
		}
		fixture.mu.Lock()
		running := fixture.running[broker.runName("task-1", id)]
		fixture.mu.Unlock()
		if !running {
			t.Fatal("fixture unexpectedly stopped despite failed removals")
		}
		if removals != attempt*2 {
			t.Fatal("repeated close did not retry both cleanup phases", removals)
		}
	}
	if response := brokerRequest(broker, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Ready"}); response.Code != 409 {
		t.Fatal("closed worker resumed while cleanup was uncertain", response.Code)
	}
	blocked = false
	if response := brokerRequest(broker, "/v1/close", closeBody(id)); response.Code != 200 {
		t.Fatal("recovered Docker cleanup did not succeed", response.Code, response.Body.String())
	}
	broker.mu.Lock()
	retained := broker.runs[runKeyName("task-1", id)] != nil
	closed := broker.closed[runKeyName("task-1", id)]
	broker.mu.Unlock()
	if retained || !closed {
		t.Fatal("confirmed cleanup did not release the run while retaining its tombstone")
	}
	fixture.mu.Lock()
	running := fixture.running[broker.runName("task-1", id)]
	fixture.mu.Unlock()
	if running {
		t.Fatal("cleanup acknowledged success while fixture worker remains running")
	}
}

func TestFinalConfirmedRemovalReleasesRunAfterEarlierFailure(t *testing.T) {
	broker := fixtureBroker(t)
	fixture := newFake()
	failFirst := false
	failed := false
	broker.dockerFn = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "rm" && failFirst && !failed {
			failed = true
			return nil, errors.New("synthetic transient first removal failure")
		}
		return fixture.call(ctx, input, args...)
	}
	defer broker.Close()
	id := strings.Repeat("e", 32)
	if response := brokerRequest(broker, "/v1/call", Request{Session: "task-1", RunID: id, Tool: "Ready"}); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	failFirst = true
	// Existing error reporting remains conservative even though the final Docker
	// acknowledgement proves termination and makes releasing the reference safe.
	if response := brokerRequest(broker, "/v1/close", closeBody(id)); response.Code != 503 {
		t.Fatal("first-phase cleanup error was hidden", response.Code)
	}
	broker.mu.Lock()
	retained := broker.runs[runKeyName("task-1", id)] != nil
	broker.mu.Unlock()
	fixture.mu.Lock()
	running := fixture.running[broker.runName("task-1", id)]
	fixture.mu.Unlock()
	if retained || running {
		t.Fatal("final acknowledged removal did not clean up the worker")
	}
	if response := brokerRequest(broker, "/v1/close", closeBody(id)); response.Code != 200 {
		t.Fatal("confirmed cleanup was not idempotent", response.Code)
	}
}

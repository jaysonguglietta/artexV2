package isolation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentCloseWaitsForAcknowledgedRemoval(t *testing.T) {
	b := fixtureBroker(t)
	id := strings.Repeat("a", 32)
	run, _, err := b.admit("task-1", id)
	if err != nil {
		t.Fatal(err)
	}
	close(run.ready)
	entered, release, overlap := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var mu sync.Mutex
	active, calls := false, 0
	b.dockerFn = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		mu.Lock()
		if active {
			mu.Unlock()
			select {
			case overlap <- struct{}{}:
			default:
			}
			return nil, errors.New("Error response from daemon: removal of container fixture is already in progress")
		}
		active = true
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		mu.Lock()
		active = false
		mu.Unlock()
		return nil, nil
	}
	results := make(chan error, 4)
	go func() { results <- b.closeRun("task-1", id) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first removal did not begin")
	}
	started := make(chan struct{}, 3)
	for range 3 {
		go func() {
			started <- struct{}{}
			results <- b.closeRun("task-1", id)
		}()
	}
	for range 3 {
		<-started
	}
	// Foreground cancellation and the close endpoint must wait for the same
	// acknowledged stop, without issuing conflicting Docker rm operations.
	select {
	case <-overlap:
		t.Error("concurrent close issued overlapping Docker removals")
	case <-results:
		t.Error("close returned before Docker acknowledged removal")
		results <- errors.New("close returned before acknowledgement")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for range 4 {
		select {
		case err := <-results:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Fatal("close did not complete after Docker acknowledgement")
		}
	}
	if _, _, err := b.admit("task-1", id); err == nil {
		t.Fatal("concurrent cleanup allowed resurrection")
	}
}

func TestRemovalConflictNeedsAcknowledgementOrConfirmedAbsence(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "absent"}[absent], func(t *testing.T) {
			b := fixtureBroker(t)
			calls := 0
			b.dockerFn = func(context.Context, []byte, ...string) ([]byte, error) {
				calls++
				if calls < 3 {
					return nil, errors.New("Error response from daemon: removal of container fixture is already in progress")
				}
				if absent {
					return nil, errors.New("Error response from daemon: No such container: fixture")
				}
				return nil, nil
			}
			if err := b.removeRun(&brokerRun{name: "fixture"}); err != nil {
				t.Fatal(err)
			}
			if calls != 3 {
				t.Fatal("conflict was treated as proof of successful removal", calls)
			}
		})
	}
}

func TestRemovalDoesNotHidePersistentConflictOrOtherDockerErrors(t *testing.T) {
	b := fixtureBroker(t)
	calls := 0
	b.dockerFn = func(ctx context.Context, _ []byte, _ ...string) ([]byte, error) {
		calls++
		<-ctx.Done()
		return nil, errors.New("Error response from daemon: removal of container fixture is already in progress")
	}
	if err := b.removeRun(&brokerRun{name: "fixture"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("persistent removal conflict did not fail at the existing deadline", err)
	}
	if calls != 1 {
		t.Fatal("expired removal operation retried Docker", calls)
	}
	for _, message := range []string{"Docker daemon unavailable", "permission denied", "container is already in progress"} {
		b.dockerFn = func(context.Context, []byte, ...string) ([]byte, error) {
			return nil, errors.New(message)
		}
		if err := b.removeRun(&brokerRun{name: "fixture"}); err == nil || err.Error() != message {
			t.Fatal("unrelated Docker error was hidden", message, err)
		}
	}
}

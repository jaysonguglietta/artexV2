package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// English task-control/intervention errors. HTTP ControlWork and detached-intent failures are distinct from model-tool/state-transition diagnostics, but all narrative is now English.

// Directly exercise the no-running-work path without a database and preserve errors.Is compatibility with errWorkControlConflict.
func TestControlWorkNoRunningWorkErrorLocalized(t *testing.T) {
	e := NewEngine(nil)
	err := e.ControlWork(context.Background(), 42, "pause")
	if err == nil {
		t.Fatal("ControlWork returned nil without active work")
	}
	if !errors.Is(err, errWorkControlConflict) {
		t.Fatalf("errors.Is(err, errWorkControlConflict) = false, err=%v", err)
	}
	assertEnglishError(t, "control_work_no_running", err.Error())
}

// Pin remaining formatted errors whose paths require engine/database setup or goroutine timing.
func TestEngineUserFacingErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"work_control_busy", fmt.Errorf(errWorkControlBusyFmt, errWorkControlConflict, int64(42), "pause").Error()},
		{"work_control_wait", fmt.Errorf(errWorkControlWaitFmt, int64(42), "pause", context.DeadlineExceeded).Error()},
		{"detached_worker_not_ready", errDetachedWorkerNotReady},
		{"detached_state_conflict", fmt.Errorf(errIntentCtrlStateConflictFmt, db.ErrIntentStateConflict).Error()},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}

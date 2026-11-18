package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Update progress/reasons/errors are rendered directly by the settings card. Pin their English text with shared response helpers.

func TestUpdateMessageConstantsLocalized(t *testing.T) {
	assertEnglishError(t, "updateMsgPreparing", updateMsgPreparing)
	assertEnglishError(t, "updateMsgFailed", updateMsgFailed)
	assertEnglishError(t, "updateMsgStaged", updateMsgStaged)
	assertEnglishError(t, "updateErrInProgress", updateErrInProgress)
	assertEnglishError(t, "updateErrRollbackInProgress", updateErrRollbackInProgress)

	// Check format constants after substituting %q/%s placeholders.
	notRelease := fmt.Sprintf(updateErrNotReleaseFmt, "v0.0.0-dev")
	assertEnglishError(t, "updateErrNotReleaseFmt", notRelease)
	if !strings.Contains(notRelease, "v0.0.0-dev") {
		t.Fatalf("updateErrNotReleaseFmt: version placeholder was not substituted: %q", notRelease)
	}
	latest := fmt.Sprintf(updateErrAlreadyLatestFmt, "v1.2.3")
	assertEnglishError(t, "updateErrAlreadyLatestFmt", latest)
	if !strings.Contains(latest, "v1.2.3") {
		t.Fatalf("updateErrAlreadyLatestFmt: version placeholder was not substituted: %q", latest)
	}
}

// Exercise begin/finish progress transitions on a local update hub without contaminating global state.
func TestUpdateProgressMessagesLocalized(t *testing.T) {
	h := &updateHub{subs: map[chan updateProgress]struct{}{}}

	if !h.begin("v1.2.3") {
		t.Fatal("First begin call must return true")
	}
	cur, running := h.snapshot()
	if !running {
		t.Fatal("Must be running after begin")
	}
	if cur.Message != updateMsgPreparing {
		t.Fatalf("Preparing message mismatch: %q", cur.Message)
	}
	if h.begin("v1.2.4") {
		t.Fatal("begin must return false while running")
	}

	h.finish(errors.New("download failed"))
	cur, running = h.snapshot()
	if running {
		t.Fatal("finish must clear running")
	}
	if cur.Message != updateMsgFailed {
		t.Fatalf("Failure message mismatch: %q", cur.Message)
	}

	h.finish(nil)
	cur, _ = h.snapshot()
	if cur.Message != updateMsgStaged {
		t.Fatalf("Ready message mismatch: %q", cur.Message)
	}
}

// The legacy opt-in and a busy update hub cannot enable production rollback.
func TestUpdateRollbackDisabledEvenWhenBusy(t *testing.T) {
	t.Setenv("ARTEX_ENABLE_SELF_UPDATE", "1")
	updHub.mu.Lock()
	prev := updHub.running
	updHub.running = true
	updHub.mu.Unlock()
	defer func() {
		updHub.mu.Lock()
		updHub.running = prev
		updHub.mu.Unlock()
	}()

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/update/rollback", nil)
	rec := httptest.NewRecorder()
	s.updateRollback(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected status 403, got %d (body %s)", rec.Code, rec.Body.Bytes())
	}
	msg := decodeErrorField(t, rec.Body.Bytes())
	assertEnglishError(t, "updateRollback disabled 403", msg)
	if !strings.Contains(msg, "signed prior release") {
		t.Fatalf("Rollback refusal message mismatch: %q", msg)
	}
}

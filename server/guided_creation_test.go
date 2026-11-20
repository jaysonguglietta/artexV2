package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestGuidedCreationDoesNotStartPlanning(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer m.Close()
	s := newAdmissionTestServer(m, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"description":"Limited HTTPS assessment of https://example.com/","goal":"Report actual evidence","start_paused":true,"seed_first_intent":true,"timeout_seconds":900}`))
	w := httptest.NewRecorder()
	s.createTask(w, r)
	if w.Code != 201 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		ID     string `json:"id"`
		Paused bool   `json:"paused"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	task, ok := m.Task(response.ID)
	if !ok {
		t.Fatal("task missing")
	}
	t.Cleanup(func() { _, _ = m.DeleteTask(task.ID, DeleteTaskOptions{}) })
	if !response.Paused || !task.lifecycleSnapshot().Paused || task.lifecycleSnapshot().FirstRunAt != 0 {
		t.Fatal("guided task was admitted before review")
	}
	intents, err := task.Store.ListByKind(db.KindIntent, 10)
	if err != nil || len(intents) != 0 {
		t.Fatalf("immediate intent escaped pause: %v %v", intents, err)
	}
	if _, started := s.engine.started.Load(task.ID); started {
		t.Fatal("engine started before operator review")
	}
}

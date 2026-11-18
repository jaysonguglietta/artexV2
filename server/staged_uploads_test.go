package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeStagedFixture(t *testing.T, base, draft, name string, data []byte) string {
	t.Helper()
	directory := filepath.Join(base, "drafts", draft, "uploads")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(directory, name)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestStagedUploadsSnapshotAndExplicitPortablePaths(t *testing.T) {
	root := t.TempDir()
	selected := writeStagedFixture(t, root, "draft_A", "attachment", []byte("selected"))
	other := writeStagedFixture(t, root, "draft_B", "attachment", []byte("other"))
	snapshot, err := readStagedUploads(root, "draft_A")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(selected, []byte("later draft edit"), 0600); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("Read `%s`; preserve %s and %s-backup", selected, other, snapshot.absolutePrefix)
	rewritten, err := snapshot.rewrite(text)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rewritten, "`/workspace/uploads/attachment`") || !strings.Contains(rewritten, other) || !strings.Contains(rewritten, snapshot.absolutePrefix+"-backup") {
		t.Fatalf("unexpected rewrite: %q", rewritten)
	}
	if err = snapshot.copyToTask(root, "42"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "tasks", "42", "uploads", "attachment"))
	if err != nil || string(got) != "selected" {
		t.Fatalf("snapshot bytes changed: %q %v", got, err)
	}
	none, err := readStagedUploads(root, "")
	if err != nil || none != nil {
		t.Fatal("empty draft must not inspect staging", err)
	}
	if got, err := none.rewrite(other); err != nil || got != other {
		t.Fatalf("unselected prose was rewritten: %q %v", got, err)
	}
}

func TestStagedUploadsRejectTraversalAndSymlinks(t *testing.T) {
	for _, draft := range []string{"..", "../other", "a/b", `a\b`, strings.Repeat("a", 129)} {
		t.Run("draft="+draft, func(t *testing.T) {
			if _, err := readStagedUploads(t.TempDir(), draft); err == nil {
				t.Fatal("unsafe draft accepted")
			}
		})
	}
	for _, component := range []string{"drafts", "drafts/chosen", "drafts/chosen/uploads", "drafts/chosen/uploads/file"} {
		t.Run(component, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			canary := filepath.Join(outside, "canary")
			if err := os.WriteFile(canary, []byte("outside-secret"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, filepath.FromSlash(component))
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			destination := outside
			if strings.HasSuffix(component, "/file") {
				destination = canary
			}
			if err := os.Symlink(destination, link); err != nil {
				t.Fatal(err)
			}
			if snapshot, err := readStagedUploads(root, "chosen"); err == nil {
				t.Fatalf("symlink accepted: %+v", snapshot)
			}
			got, err := os.ReadFile(canary)
			if err != nil || string(got) != "outside-secret" {
				t.Fatal("outside canary changed")
			}
		})
	}
	t.Run("inside-root draft alias", func(t *testing.T) {
		root := t.TempDir()
		writeStagedFixture(t, root, "other", "file", []byte("other draft"))
		if err := os.Symlink("other", filepath.Join(root, "drafts", "chosen")); err != nil {
			t.Fatal(err)
		}
		if _, err := readStagedUploads(root, "chosen"); err == nil {
			t.Fatal("another in-root draft was imported through a symlink")
		}
	})
}

func TestStagedUploadsRejectBudgetAndSubdirectoriesBeforeCreation(t *testing.T) {
	root := t.TempDir()
	writeStagedFixture(t, root, "large", "file", make([]byte, maxStagedUploadBytes+1))
	s := &Server{m: &Manager{dir: root}}
	request := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"description":"budget test","draft_id":"large"}`))
	recorder := httptest.NewRecorder()
	s.createTask(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "4 MiB") {
		t.Fatalf("want pre-database budget rejection, got %d %s", recorder.Code, recorder.Body.String())
	}
	nested := filepath.Join(root, "drafts", "nested", "uploads", "subdir")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readStagedUploads(root, "nested"); err == nil {
		t.Fatal("nested staged directory accepted")
	}
	missing, err := readStagedUploads(root, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = missing.rewrite(filepath.Join(root, "drafts", "empty", "uploads", "missing")); err == nil {
		t.Fatal("missing referenced attachments accepted")
	}
}

func TestStagedUploadsEnforceCombinedBudgetAndFileCount(t *testing.T) {
	t.Run("combined byte budget", func(t *testing.T) {
		root := t.TempDir()
		writeStagedFixture(t, root, "draft", "first", make([]byte, maxStagedUploadBytes/2))
		writeStagedFixture(t, root, "draft", "second", make([]byte, maxStagedUploadBytes/2))
		if _, err := readStagedUploads(root, "draft"); err != nil {
			t.Fatalf("exact transfer budget should be accepted: %v", err)
		}
		writeStagedFixture(t, root, "draft", "third", []byte("x"))
		if _, err := readStagedUploads(root, "draft"); err == nil {
			t.Fatal("combined transfer budget was not enforced")
		}
	})
	t.Run("empty-file count", func(t *testing.T) {
		root := t.TempDir()
		for index := 0; index <= maxStagedUploadFiles; index++ {
			writeStagedFixture(t, root, "draft", fmt.Sprintf("file-%d", index), nil)
		}
		if _, err := readStagedUploads(root, "draft"); err == nil {
			t.Fatal("unbounded empty staged files accepted")
		}
	})
}

func TestStagedUploadsCopyRejectsExistingAndEscapedDestination(t *testing.T) {
	for _, component := range []string{"tasks", "tasks/42"} {
		t.Run(component, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			writeStagedFixture(t, root, "draft", "file", []byte("attachment"))
			link := filepath.Join(root, filepath.FromSlash(component))
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			snapshot, err := readStagedUploads(root, "draft")
			if err != nil {
				t.Fatal(err)
			}
			if err = snapshot.copyToTask(root, "42"); err == nil {
				t.Fatal("symlink destination accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("outside destination changed", err)
			}
		})
	}
	root := t.TempDir()
	writeStagedFixture(t, root, "draft", "file", []byte("attachment"))
	existing := filepath.Join(root, "tasks", "42")
	if err := os.MkdirAll(existing, 0700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(existing, "keep")
	if err := os.WriteFile(canary, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readStagedUploads(root, "draft")
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.copyToTask(root, "42"); err == nil {
		t.Fatal("preexisting task directory accepted")
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "keep" {
		t.Fatal("preexisting task data changed")
	}
}

func TestStagingUploadCreateTaskCopiesBeforeLaunch(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		if os.Getenv("ARTEX_ISOLATED_TEST_DATABASE") == "1" {
			t.Fatal(err)
		}
		t.Skipf("isolated PostgreSQL unavailable: %v", err)
	}
	defer m.Close()
	s := newAdmissionTestServer(m, func(*Task) bool { return false })
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	field, err := form.CreateFormFile("file", "attachment")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = field.Write([]byte("synthetic staged attachment")); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest("POST", "/api/chat/upload?scope=staging&id=draft_create", &body)
	upload.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.chatUpload(recorder, upload)
	if recorder.Code != 200 {
		t.Fatalf("staging upload: %d %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Attachments []chatAttachment `json:"attachments"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || len(result.Attachments) != 1 {
		t.Fatal(result, err)
	}
	attachment := result.Attachments[0]
	payload, _ := json.Marshal(createTaskReq{Name: "Staging transfer", Description: "Read `" + attachment.Abs + "`", Goal: "Inspect " + attachment.Abs, DraftID: "draft_create"})
	recorder = httptest.NewRecorder()
	s.createTask(recorder, httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(payload)))
	if recorder.Code != 201 {
		t.Fatalf("create: %d %s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatal(created, err)
	}
	defer m.DeleteTask(created.ID, DeleteTaskOptions{DeleteFiles: true})
	task, ok := m.Task(created.ID)
	if !ok {
		t.Fatal("created task is missing")
	}
	if strings.Contains(task.Description, attachment.Abs) || strings.Contains(task.Goal, attachment.Abs) || !strings.Contains(task.Description, "/workspace/uploads/attachment") || !strings.Contains(task.Goal, "/workspace/uploads/attachment") {
		t.Fatalf("task retains wrong paths: %q / %q", task.Description, task.Goal)
	}
	got, err := os.ReadFile(filepath.Join(m.dir, "tasks", created.ID, "uploads", "attachment"))
	if err != nil || string(got) != "synthetic staged attachment" {
		t.Fatalf("task attachment unavailable at create response: %q %v", got, err)
	}
	numeric, _ := strconv.ParseInt(created.ID, 10, 64)
	stored, err := m.pg.GetTask(numeric)
	if err != nil || stored == nil || stored.Description != task.Description || stored.Goal != task.Goal {
		t.Fatal("portable paths not persisted", err)
	}
}

func TestStagingCopyFailureReturnsCreatedPausedTask(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		if os.Getenv("ARTEX_ISOLATED_TEST_DATABASE") == "1" {
			t.Fatal(err)
		}
		t.Skipf("isolated PostgreSQL unavailable: %v", err)
	}
	defer m.Close()
	writeStagedFixture(t, m.dir, "draft_failure", "attachment", []byte("preserve source"))
	if err = os.WriteFile(filepath.Join(m.dir, "tasks"), []byte("block task directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newAdmissionTestServer(m, func(*Task) bool { return false })
	recorder := httptest.NewRecorder()
	s.createTask(recorder, httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"description":"Copy failure fixture","goal":"No execution","draft_id":"draft_failure"}`)))
	if recorder.Code != 500 {
		t.Fatalf("want explicit partial commit, got %d %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		ID             string `json:"task_id"`
		Created        bool   `json:"created"`
		PausePersisted bool   `json:"pause_persisted"`
		Error          string `json:"error"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.ID == "" || !result.Created || !result.PausePersisted || !strings.Contains(result.Error, "Task #"+result.ID) {
		t.Fatal("missing actionable created-task details", result, err)
	}
	defer m.DeleteTask(result.ID, DeleteTaskOptions{})
	numeric, _ := strconv.ParseInt(result.ID, 10, 64)
	task, err := m.pg.GetTask(numeric)
	if err != nil || task == nil || !task.Paused {
		t.Fatal("partially created task was not paused", err)
	}
	if s.engine.Started(result.ID) {
		t.Fatal("task execution started before attachment transfer")
	}
	got, err := os.ReadFile(filepath.Join(m.dir, "drafts", "draft_failure", "uploads", "attachment"))
	if err != nil || string(got) != "preserve source" {
		t.Fatal("source attachment changed", err)
	}
}

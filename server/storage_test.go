package server

import (
	"bytes"
	"encoding/json"
	"errors"
	pgdb "github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/storage"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storageServerFixture(t *testing.T, capacity int64) *Server {
	t.Helper()
	directory := t.TempDir()
	if _, err := storage.Open(directory, storage.Workspace, storage.Limits{Bytes: capacity, Entries: 1000}, "evidence", "traffic"); err != nil {
		t.Fatal(err)
	}
	return &Server{m: &Manager{dir: directory}}
}
func TestWorkspaceQuotaReturns507WithoutChangingOriginal(t *testing.T) {
	s := storageServerFixture(t, 64)
	path := filepath.Join(s.m.dir, "original")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"path": "original", "content": strings.Repeat("x", 80)})
	request := httptest.NewRequest(http.MethodPost, "/api/workspace/write", bytes.NewReader(body))
	response := httptest.NewRecorder()
	s.wsWrite(response, request)
	if response.Code != 507 || !strings.Contains(response.Body.String(), "delete unheld data") {
		t.Fatalf("quota refusal was not actionable: %d %s", response.Code, response.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "keep" {
		t.Fatal("rejected overwrite changed original data")
	}
}
func TestWorkspaceCannotMutateManagedRootsOrSymlinkAliases(t *testing.T) {
	s := storageServerFixture(t, 4096)
	evidenceDir := filepath.Join(s.m.dir, "evidence")
	if err := os.Mkdir(evidenceDir, 0700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(evidenceDir, "keep")
	if err := os.WriteFile(canary, []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(evidenceDir, filepath.Join(s.m.dir, "alias")); err != nil {
		t.Skip(err)
	}
	for _, path := range []string{"evidence/keep", "traffic/_index/index.sqlite", "archives/case.tar.zst", "_storage-holds.json", "alias/keep"} {
		response := httptest.NewRecorder()
		s.wsDelete(response, httptest.NewRequest(http.MethodDelete, "/api/workspace/delete?path="+path, nil))
		if response.Code < 400 {
			t.Fatalf("managed deletion accepted: %s", path)
		}
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "protected" {
		t.Fatal("managed evidence changed")
	}
}
func TestStorageHoldAPIRequiresReleaseConfirmationAndBlocksFileMutation(t *testing.T) {
	s := storageServerFixture(t, 4096)
	path := filepath.Join(s.m.dir, "keep")
	if err := os.WriteFile(path, []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.addStorageHold(response, httptest.NewRequest(http.MethodPost, "/api/storage/holds", strings.NewReader(`{"kind":"workspace","reason":"preserve case artifacts"}`)))
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var hold storage.Hold
	if err := json.Unmarshal(response.Body.Bytes(), &hold); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.wsDelete(response, httptest.NewRequest(http.MethodDelete, "/api/workspace/delete?path=keep", nil))
	if response.Code != 409 {
		t.Fatalf("hold bypass status=%d", response.Code)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/storage/holds/"+hold.ID, strings.NewReader(`{"kind":"workspace","confirmation":"wrong"}`))
	request.SetPathValue("id", hold.ID)
	response = httptest.NewRecorder()
	s.releaseStorageHold(response, request)
	if response.Code != 400 {
		t.Fatal("hold released without explicit confirmation")
	}
	request = httptest.NewRequest(http.MethodDelete, "/api/storage/holds/"+hold.ID, strings.NewReader(`{"kind":"workspace","confirmation":"RELEASE LEGAL HOLD"}`))
	request.SetPathValue("id", hold.ID)
	response = httptest.NewRecorder()
	s.releaseStorageHold(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	response = httptest.NewRecorder()
	s.wsDelete(response, httptest.NewRequest(http.MethodDelete, "/api/workspace/delete?path=keep", nil))
	if response.Code != 200 {
		t.Fatalf("unheld deletion failed: %d %s", response.Code, response.Body.String())
	}
}
func TestStreamingUploadQuotaNeverPublishesPartialFiles(t *testing.T) {
	s := storageServerFixture(t, 4096)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(bytes.Repeat([]byte{'x'}, 8192)); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/workspace/upload", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	s.wsUpload(response, request)
	if response.Code != 507 {
		t.Fatalf("streaming quota bypass: %d %s", response.Code, response.Body.String())
	}
	if _, err = os.Lstat(filepath.Join(s.m.dir, "large.bin")); !os.IsNotExist(err) {
		t.Fatal("partial upload was published")
	}
	entries, err := os.ReadDir(filepath.Join(s.m.dir, "_upload-staging"))
	if err != nil || len(entries) != 0 {
		t.Fatal("failed upload staging was not cleaned", err)
	}
}
func TestArchiveAndTaskDestructionRespectHoldsBeforeAnyDatabaseAccess(t *testing.T) {
	s := storageServerFixture(t, 4096)
	bucket, err := storage.Open(filepath.Join(s.m.dir, "evidence"), storage.Evidence, storage.Limits{Bytes: 4096, Entries: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bucket.AddHold("retain complete evidence metadata"); err != nil {
		t.Fatal(err)
	}
	job := &pgdb.TaskArchive{ID: 1, TaskID: 1}
	for _, run := range []func() error{func() error { return s.archiveTask(job) }, func() error { return s.restoreTaskArchive(job) }, func() error { return s.deleteTaskArchive(job) }, func() error {
		_, err := s.m.DeleteTask("1", DeleteTaskOptions{DeleteFiles: true, DeleteFindings: true})
		return err
	}} {
		if err := run(); !errors.Is(err, storage.ErrHold) {
			t.Fatalf("destructive action bypassed hold: %v", err)
		}
	}
}
func TestRawTranscriptDefaultAndInvalidUnboundedModeFailClosed(t *testing.T) {
	t.Setenv("ARTEX_RAW_TRANSCRIPTS", "")
	s := storageServerFixture(t, 4096)
	if store := s.rawTranscriptStore(); store != nil {
		t.Fatal("ordinary binary enabled unbounded SDK transcript persistence")
	}
	t.Setenv("ARTEX_RAW_TRANSCRIPTS", "persistent")
	if err := storage.ValidateRawTranscripts(s.m.dir); err == nil {
		t.Fatal("unbounded raw transcript mode accepted")
	}
}

func TestStorageOperatorRoutesRequireAuthentication(t *testing.T) {
	s := storageServerFixture(t, 4096)
	mux := http.NewServeMux()
	s.registerStorage(mux)
	handler := s.requireAuth(mux)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/storage"}, {"POST", "/api/storage/holds"}, {"DELETE", "/api/storage/holds/not-a-hold"}, {"POST", "/api/storage/purge"}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`)))
		if response.Code != 401 {
			t.Fatalf("unauthenticated storage operation accepted: %s %s %d", endpoint.method, endpoint.path, response.Code)
		}
	}
}

func TestRawTranscriptWriterRetainsDiskErrorWithoutPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked-store")
	if err := os.WriteFile(path, []byte("ordinary-file"), 0600); err != nil {
		t.Fatal(err)
	}
	writer := transcript.NewStore(path).NewWriter("session", "")
	writer.RecordMessage(llm.Message{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "do not lose primary conversation"}}}, llm.Usage{})
	if writer.Err() == nil {
		t.Fatal("SDK trace append failure was not retained")
	}
	writer.RecordMessage(llm.Message{Role: "assistant"}, llm.Usage{})
	if writer.Err() == nil {
		t.Fatal("SDK trace failure unexpectedly disappeared")
	}
}

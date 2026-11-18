package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "canary")
	if err := os.WriteFile(secret, []byte("outside-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skip(err)
	}
	s := &Server{m: &Manager{dir: base}}
	for _, method := range []string{"read", "download", "write", "delete", "mkdir"} {
		w := httptest.NewRecorder()
		switch method {
		case "read":
			s.wsRead(w, httptest.NewRequest("GET", "/api/workspace/read?path=link/canary", nil))
		case "download":
			s.wsDownload(w, httptest.NewRequest("GET", "/api/workspace/download?path=link/canary", nil))
		case "write":
			s.wsWrite(w, httptest.NewRequest("POST", "/api/workspace/write", strings.NewReader(`{"path":"link/canary","content":"changed"}`)))
		case "delete":
			s.wsDelete(w, httptest.NewRequest("DELETE", "/api/workspace/delete?path=link/canary", nil))
		case "mkdir":
			s.wsMkdir(w, httptest.NewRequest("POST", "/api/workspace/mkdir", strings.NewReader(`{"path":"link/new"}`)))
		}
		if w.Code < 400 {
			t.Errorf("%s accepted symlink escape: %d", method, w.Code)
		}
		if strings.Contains(w.Body.String(), "outside-canary") {
			t.Fatal("secret disclosed")
		}
	}
	data, err := os.ReadFile(secret)
	if err != nil || string(data) != "outside-canary" {
		t.Fatalf("outside file changed: %s %v", data, err)
	}
	// Normal nested writes and reads still work.
	w := httptest.NewRecorder()
	s.wsWrite(w, httptest.NewRequest("POST", "/api/workspace/write", strings.NewReader(`{"path":"nested/note.txt","content":"hello"}`)))
	if w.Code != 200 {
		t.Fatalf("normal write: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.wsRead(w, httptest.NewRequest("GET", "/api/workspace/read?path=nested/note.txt", nil))
	var result struct{ Content string }
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Content != "hello" {
		t.Fatalf("normal read: %s", w.Body.String())
	}
}

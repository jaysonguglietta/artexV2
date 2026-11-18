package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// English chat-upload response regression coverage, reusing assertEnglishError and decodeErrorField.

// Pin all seven response constants, including prefixes followed by original underlying errors.
func TestChatUploadErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"scope_invalid": errChatUploadScopeInvalid,
		"bad_id":        errChatUploadBadID,
		"task_deleting": errChatUploadTaskDeleting,
		"mkdir":         errChatUploadMkdir,
		"parse":         errChatUploadParse,
		"no_file":       errChatUploadNoFile,
		"save_failed":   errChatUploadSaveFailed,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// Exercise database-free HTTP upload validation and deletion barriers. Session uploads need only a temporary workspace; task deletion uses the engine deletion gate.
func TestChatUploadHandlerResponsesLocalized(t *testing.T) {
	// A multipart body with fields but no file parses successfully yet contains zero uploads.
	noFileBody := func() (*bytes.Buffer, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("other", "x")
		_ = mw.Close()
		return &buf, mw.FormDataContentType()
	}

	t.Run("scope-invalid", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=bogus&id=x", nil)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadScopeInvalid {
			t.Fatalf("response=%q, want %q", got, errChatUploadScopeInvalid)
		}
		assertEnglishError(t, "scope-invalid", errChatUploadScopeInvalid)
	})

	t.Run("bad-id", func(t *testing.T) {
		s := &Server{}
		// Session scope reaches ID validation before accessing Manager or PostgreSQL.
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=session&id=../evil", nil)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadBadID {
			t.Fatalf("response=%q, want %q", got, errChatUploadBadID)
		}
		assertEnglishError(t, "bad-id", errChatUploadBadID)
	})

	t.Run("no-file", func(t *testing.T) {
		s := &Server{m: &Manager{dir: t.TempDir()}}
		body, ctype := noFileBody()
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=session&id=sess1", body)
		req.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadNoFile {
			t.Fatalf("response=%q, want %q", got, errChatUploadNoFile)
		}
		assertEnglishError(t, "no-file", errChatUploadNoFile)
	})

	t.Run("task-deleting", func(t *testing.T) {
		s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
		s.engine.deleting.Store("t1", true)
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=task&id=t1", nil)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d, want 409 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadTaskDeleting {
			t.Fatalf("response=%q, want %q", got, errChatUploadTaskDeleting)
		}
		assertEnglishError(t, "task-deleting", errChatUploadTaskDeleting)
	})
}

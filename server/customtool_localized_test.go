package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// English custom-tool CRUD/editor validation coverage. Tool-result semantics and raw payloads remain separate from user-facing error constants.

// Pin eight response constants, including database-gated duplicate/edit checks not reachable in the database-free HTTP fixtures.
func TestCustomToolErrorConstantsLocalized(t *testing.T) {
	for _, c := range []struct{ name, msg string }{
		{"keyFormat", errCustomToolKeyFormat},
		{"kindInvalid", errCustomToolKindInvalid},
		{"httpSchemaRequired", errCustomToolHTTPSchemaRequired},
		{"keyExists", errCustomToolKeyExists},
		{"editCustomOnly", errCustomToolEditCustomOnly},
		{"badBody", errCustomToolBadBody},
		{"shellNoExec", errCustomToolShellNoExec},
		{"unknownKindPrefix", errCustomToolUnknownKindPrefix},
	} {
		assertEnglishError(t, "customtool."+c.name, c.msg)
	}
}

// newCustomToolServer passes the nonnil pg gate using an empty DB wrapper; every tested branch returns before dereferencing its nil embedded sql.DB.
func newCustomToolServer() *Server {
	return &Server{m: &Manager{pg: &db.DB{}}}
}

// Exercise three create validation paths through actual HTTP responses, before any GetTool lookup.
func TestCustomToolCreateValidationLocalized(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"bad-key", `{"key":"BadKey","kind":"command"}`, errCustomToolKeyFormat},
		{"bad-kind", `{"key":"goodkey","kind":"bogus"}`, errCustomToolKindInvalid},
		{"http-no-schema", `{"key":"goodkey","kind":"http"}`, errCustomToolHTTPSchemaRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newCustomToolServer()
			req := httptest.NewRequest(http.MethodPost, "/api/tools/custom", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			s.pgCreateCustomTool(rec, req)
			if rec.Code != 400 && rec.Code != 409 {
				t.Fatalf("Expected validation status, got %d (body %q)", rec.Code, rec.Body.String())
			}
			got := decodeErrorField(t, rec.Body.Bytes())
			if got != c.want {
				t.Fatalf("Response mismatch: got %q want %q", got, c.want)
			}
			assertEnglishError(t, "customtool.create."+c.name, got)
		})
	}
}

// Exercise three editor-test validation paths through actual HTTP responses without database calls.
func TestCustomToolTestRunValidationLocalized(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"bad-body", `not-json`, errCustomToolBadBody},
		{"shell", `{"kind":"shell"}`, errCustomToolShellNoExec},
		{"unknown-kind", `{"kind":"bogus"}`, errCustomToolUnknownKindPrefix + "bogus"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newCustomToolServer()
			req := httptest.NewRequest(http.MethodPost, "/api/tools/custom/test", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			s.pgTestCustomTool(rec, req)
			if rec.Code != 400 {
				t.Fatalf("Expected status 400, got %d (body %q)", rec.Code, rec.Body.String())
			}
			got := decodeErrorField(t, rec.Body.Bytes())
			if got != c.want {
				t.Fatalf("Response mismatch: got %q want %q", got, c.want)
			}
			// The unknown-kind error appends the supplied ASCII kind; still require no untranslated narrative.
			assertEnglishError(t, "customtool.test."+c.name, errCustomToolUnknownKindPrefix)
		})
	}
}

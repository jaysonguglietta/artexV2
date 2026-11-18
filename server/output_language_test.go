package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/artex/config"
)

// TestOutputLanguageCacheFollowsSettings pins that the cached output language
// (read by every writeErr and label) is refreshed by PUT /api/settings, so a
// switch applies to the very next response without re-reading the settings
// table per message.
func TestOutputLanguageCacheFollowsSettings(t *testing.T) {
	t.Setenv("ARTEX_LANG", "")
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()
	prev, hadPrev, _ := m.pg.GetSetting(settingOutputLanguage)
	t.Cleanup(func() { config.CurrentLanguage = nil })
	// defer (not t.Cleanup) so the restore runs before the deferred m.Close.
	defer func() {
		if hadPrev {
			_ = m.pg.SetSetting(settingOutputLanguage, prev)
		} else {
			_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingOutputLanguage)
		}
	}()
	_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingOutputLanguage)

	s := New(context.Background(), m, t.TempDir(), t.TempDir(), t.TempDir())
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	errOf := func(rec *httptest.ResponseRecorder) string {
		var v struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return v.Error
	}

	if got := s.outputLanguage(); got != "en" {
		t.Fatalf("default output language = %q, want en", got)
	}
	for _, lang := range []string{"ko", "zh", "es", "ja", "fr"} {
		rec := do(http.MethodPut, "/api/settings", map[string]any{"output_language": lang})
		if rec.Code != 400 {
			t.Fatalf("unsupported locale %s status=%d", lang, rec.Code)
		}
		assertEnglishError(t, "unsupported locale", errOf(rec))
	}
	if got := s.outputLanguage(); got != "en" {
		t.Fatalf("output language changed: %s", got)
	}
	if rec := do(http.MethodPut, "/api/settings", map[string]any{"output_language": "en"}); rec.Code != 200 {
		t.Fatalf("set English: %d %s", rec.Code, rec.Body.String())
	}
}

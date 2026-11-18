package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// English authentication regression coverage: exercise requireAuth HTTP responses without PostgreSQL and pin the remaining handler constants.

// Verify missing/invalid-token responses are HTTP 401 with English bodies through the real middleware; it does not require a database.
func TestRequireAuthMessagesLocalized(t *testing.T) {
	s := &Server{jwtKey: []byte(strings.Repeat("k", 32))}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := s.requireAuth(next)

	cases := []struct {
		name   string
		bearer string
		want   string
	}{
		{"missing token", "", authErrUnauthorized},
		{"invalid token", "Bearer not-a-valid-token", authErrTokenInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
			if c.bearer != "" {
				r.Header.Set("Authorization", c.bearer)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d, want 401", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, c.want) {
				t.Errorf("Response body lacks %q: %s", c.want, body)
			}
			assertEnglishError(t, c.name, body)
		})
	}
}

// Pin user-facing authentication constants for handlers whose database gates cannot be exercised here.
func TestAuthErrorConstantsLocalized(t *testing.T) {
	consts := map[string]string{
		"authErrUnauthorized":          authErrUnauthorized,
		"authErrTokenInvalid":          authErrTokenInvalid,
		"authErrPasswordAlreadySet":    authErrPasswordAlreadySet,
		"authErrPasswordEmpty":         authErrPasswordEmpty,
		"authErrNewPasswordEmpty":      authErrNewPasswordEmpty,
		"authErrPasswordHash":          authErrPasswordHash,
		"authErrSaveFailedPrefix":      authErrSaveFailedPrefix,
		"authErrTokenGen":              authErrTokenGen,
		"authErrBadRequest":            authErrBadRequest,
		"authErrPasswordNotInit":       authErrPasswordNotInit,
		"authErrCurrentPasswordWrong":  authErrCurrentPasswordWrong,
		"authErrBadCredential":         authErrBadCredential,
		"authErrPasswordTooShort":      authErrPasswordTooShort,
		"authErrSetupTokenInvalid":     authErrSetupTokenInvalid,
		"authErrTooManyAttempts":       authErrTooManyAttempts,
		"authErrPasswordTooLong":       authErrPasswordTooLong,
		"authErrDataSourceUnavailable": authErrDataSourceUnavailable,
	}
	for name, msg := range consts {
		assertEnglishError(t, name, msg)
	}
}

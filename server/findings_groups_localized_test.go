package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// English grouped-findings response regression coverage.

// Exercise a real 413 response at MaxBytesReader decoding before the database gate using an empty server.
func TestDeepenFindingBodyTooLargeLocalized(t *testing.T) {
	s := &Server{}
	body := `{"description":"` + strings.Repeat("a", 33<<10) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/exploration/findings/1/deepen", strings.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	s.deepenFinding(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d (body %q)", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Response JSON parsing failed: %v (body %q)", err, rec.Body.String())
	}
	const want = "The request body is too large"
	if resp.Error != want {
		t.Fatalf("response=%q, want %q", resp.Error, want)
	}
	assertEnglishError(t, "body_too_large", resp.Error)
}

// Pin the display-only follow-up activity summary independently of user-authored intent description and database transaction behavior.
func TestFindingFollowUpAuditSummaryLocalized(t *testing.T) {
	assertEnglishError(t, "follow_up_summary", auditFindingFollowUpSummary)
}

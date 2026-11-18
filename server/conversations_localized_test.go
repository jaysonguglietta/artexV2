package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// English conversation API error regression coverage.

// Pin twelve response constants, formatting numeric placeholders before checking the result because database-gated handlers cannot run here.
func TestConversationErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"request_too_large", convErrRequestTooLarge},
		{"agent_key_empty", convErrAgentKeyEmpty},
		{"agent_key_too_long", fmt.Sprintf(convErrAgentKeyTooLong, maxConversationAgentKeyRunes)},
		{"agent_not_found", convErrAgentNotFound},
		{"llm_profile", convErrLLMProfile},
		{"title_too_long", fmt.Sprintf(convErrTitleTooLong, maxConversationTitleRunes)},
		{"title_or_pinned", convErrTitleOrPinned},
		{"title_empty", convErrTitleEmpty},
		{"bad_conv_id", convErrBadConvID},
		{"ids_count", fmt.Sprintf(convErrIDsCount, maxConversationDeleteBatch)},
		{"message_empty", convErrMessageEmpty},
		{"busy", convErrBusy},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// Verify the database-free request decoder returns 413 with its English error when the body exceeds its limit.
func TestDecodeConversationRequestTooLargeLocalized(t *testing.T) {
	// Valid JSON beyond the 64 KiB limit exercises MaxBytesReader during decoding.
	body := `{"title":"` + strings.Repeat("a", maxConversationRequestBytes+1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(body))
	rec := httptest.NewRecorder()
	var dst struct {
		Title string `json:"title"`
	}
	if decodeConversationRequest(rec, req, &dst) {
		t.Fatal("Oversized body unexpectedly passed decodeConversationRequest")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Response JSON parsing failed: %v (body=%q)", err, rec.Body.String())
	}
	if resp.Error != convErrRequestTooLarge {
		t.Fatalf("response error=%q, want %q", resp.Error, convErrRequestTooLarge)
	}
	assertEnglishError(t, "decode.too_large.response", resp.Error)
}

// Default conversation and attachment titles must be English and distinct. The default title is also the auto-title sentinel.
func TestConversationDefaultTitlesLocalized(t *testing.T) {
	assertEnglishError(t, "default_title", convDefaultTitle)
	assertEnglishError(t, "attachment_title", convAttachmentTitle)
	if convDefaultTitle == convAttachmentTitle {
		t.Fatal("Default and attachment titles must differ")
	}
}

// Pin the shared initial-title/auto-title contract: empty and seeded titles may be replaced, while user titles may not.
func TestIsDefaultConversationTitle(t *testing.T) {
	if !isDefaultConversationTitle("") {
		t.Fatal("Empty title must permit auto-titling")
	}
	if !isDefaultConversationTitle(convDefaultTitle) {
		t.Fatalf("Seeded title %q must permit auto-titling", convDefaultTitle)
	}
	if isDefaultConversationTitle("User-authored title") {
		t.Fatal("A user-authored title must not permit auto-titling")
	}
}

// Retest terminal reasons are displayed directly from finding_retests.error and must remain English.
func TestConversationRetestReasonsLocalized(t *testing.T) {
	assertEnglishError(t, "retest_failed_to_start", convRetestFailedToStart)
	assertEnglishError(t, "retest_status_read_failed", convRetestStatusReadFailed)
	assertEnglishError(t, "retest_stopped_or_closed", convRetestStoppedOrClosed)
}

// Transcript failure wrappers for chat and main-agent runs share ASCII English framing while preserving the original underlying error.
func TestTranscriptErrorSummaryLocalized(t *testing.T) {
	cases := []struct {
		name  string
		label string
		errm  string
		want  string
	}{
		{"chat_turn", "", "connection reset", "(Error: connection reset)"},
		{"main_agent", "Main agent", "connection reset", "(Main agent Error: connection reset)"},
	}
	for _, c := range cases {
		got := transcriptErrorSummary(c.label, c.errm)
		if got != c.want {
			t.Fatalf("%s: transcriptErrorSummary=%q, want %q", c.name, got, c.want)
		}
		if !strings.Contains(got, c.errm) {
			t.Fatalf("%s: original error was not preserved: %q", c.name, got)
		}
		// Verify ASCII parentheses/colon replace full-width punctuation.
		if strings.ContainsAny(got, "（）：") {
			t.Fatalf("%s: full-width punctuation remains: %q", c.name, got)
		}
		// Check the English wrapper independently of the underlying diagnostic, fixed to ASCII here.
		wrapper := strings.ReplaceAll(got, c.errm, "")
		assertEnglishError(t, c.name+".wrapper", wrapper)
	}
}

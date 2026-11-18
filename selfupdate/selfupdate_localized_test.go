package selfupdate

// Test English update progress/errors exposed through SSE, API errors and boot notices. Keep lower-level diagnostic causes intact while rejecting untranslated framing.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func hasHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

func hasHanzi(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// assertEnglishError requires a nonnil error and rejects untranslated Han/Hangul narrative while allowing standard-library English causes.
func assertEnglishError(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error, got nil", label)
	}
	msg := err.Error()
	if hasHangul(msg) {
		t.Errorf("%s: contains untranslated Hangul: %q", label, msg)
	}
	if hasHanzi(msg) {
		t.Errorf("%s: contains untranslated Han: %q", label, msg)
	}
}

// Inject synthetic HTTP responses without contacting GitHub.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubClient(status int, body string) *http.Client {
	return &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

// Test both URL-rejection paths for English errors, beyond merely asserting rejection.
func TestCheckURLErrorsLocalized(t *testing.T) {
	nonHTTPS := checkURL(mustParse(t, "http://api.github.com/x"))
	assertEnglishError(t, "non-https", nonHTTPS)
	if !strings.Contains(nonHTTPS.Error(), "HTTPS") {
		t.Errorf("non-HTTPS error lacks HTTPS: %q", nonHTTPS.Error())
	}

	nonGitHub := checkURL(mustParse(t, "https://evil.example.com/x"))
	assertEnglishError(t, "non-github-host", nonGitHub)
	if !strings.Contains(nonGitHub.Error(), "GitHub") {
		t.Errorf("unapproved-host error lacks GitHub: %q", nonGitHub.Error())
	}
}

// Test English latest-release failures using a synthetic transport, including status, parsing and missing-tag cases.
func TestFetchLatestErrorsLocalized(t *testing.T) {
	ctx := context.Background()

	_, errRate := FetchLatest(ctx, stubClient(http.StatusForbidden, ""))
	assertEnglishError(t, "rate-limited", errRate)
	if !strings.Contains(errRate.Error(), "rate limit") {
		t.Errorf("Rate-limit error lacks an explanation: %q", errRate.Error())
	}

	_, errNF := FetchLatest(ctx, stubClient(http.StatusNotFound, ""))
	assertEnglishError(t, "not-found", errNF)

	_, errStatus := FetchLatest(ctx, stubClient(http.StatusInternalServerError, ""))
	assertEnglishError(t, "bad-status", errStatus)
	if !strings.Contains(errStatus.Error(), "GitHub") {
		t.Errorf("status error lacks GitHub: %q", errStatus.Error())
	}

	_, errJSON := FetchLatest(ctx, stubClient(http.StatusOK, "{not json"))
	assertEnglishError(t, "bad-json", errJSON)

	_, errTag := FetchLatest(ctx, stubClient(http.StatusOK, "{}"))
	assertEnglishError(t, "missing-tag", errTag)
	if !strings.Contains(errTag.Error(), "tag") {
		t.Errorf("missing-tag error lacks tag: %q", errTag.Error())
	}
}

// A missing platform package stops Stage with an English error before downloading; directory writability errors may precede it depending on the fixture environment.
func TestStageMissingAssetLocalized(t *testing.T) {
	rel := &Release{TagName: "v9.9.9"} // No release attachments.
	err := Stage(context.Background(), &http.Client{}, rel, "0.0.1", nil)
	assertEnglishError(t, "stage-missing-asset", err)
}

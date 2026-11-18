package notify

import (
	"net/url"
	"testing"
	"time"
)

// Independent OpenSSL vectors verify algorithms rather than reproducing the implementation.
// With TS=1700000000000 and SECRET=SECtest123:
//
// DingTalk:
//   printf '%s\n%s' "$TS" "$SECRET" | openssl dgst -sha256 -hmac "$SECRET" -binary | openssl base64 -A
//   expected: w3RMHXzixTMdzr8OHJUmVLS4IoPJVdu+Ut1LE48MePE=
// Feishu:
//   printf '' | openssl dgst -sha256 -hmac "$(printf '%s\n%s' "$TS" "$SECRET")" -binary | openssl base64 -A
//   expected: Hd4xFWQU6R6ad4nzy4ETIznzlqebqH7xcTFVmONTudo=
const (
	signTestTSMillis = int64(1700000000000)
	signTestSecret   = "SECtest123"
	dingTalkExpected = "w3RMHXzixTMdzr8OHJUmVLS4IoPJVdu+Ut1LE48MePE="
	feishuExpected   = "Hd4xFWQU6R6ad4nzy4ETIznzlqebqH7xcTFVmONTudo="
)

func TestDingTalkSignMatchesReference(t *testing.T) {
	got, err := dingTalkSignedURL("https://oapi.dingtalk.com/robot/send?access_token=tok", signTestSecret, time.UnixMilli(signTestTSMillis))
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("signed URL cannot parse: %v", err)
	}
	q := u.Query()
	if q.Get("sign") != dingTalkExpected {
		t.Errorf("signature mismatch\nwant %s\ngot %s", dingTalkExpected, q.Get("sign"))
	}
	if q.Get("timestamp") != "1700000000000" {
		t.Errorf("timestamp should remain milliseconds, got %q", q.Get("timestamp"))
	}
	// Preserve existing access_token query parameters.
	if q.Get("access_token") != "tok" {
		t.Errorf("existing query parameter lost: %q", q.Get("access_token"))
	}
}

func TestFeishuSignMatchesReference(t *testing.T) {
	got := feishuSign("1700000000000", signTestSecret)
	if got != feishuExpected {
		t.Errorf("signature mismatch\nwant %s\ngot %s", feishuExpected, got)
	}
}

// DingTalk keys HMAC with secret; Feishu keys it with the signing string and an empty message. Refactoring must not merge their different algorithms.
func TestSignAlgorithmsDiffer(t *testing.T) {
	ts := "1700000000000"
	dingURL, err := dingTalkSignedURL("https://example.com/hook", signTestSecret, time.UnixMilli(signTestTSMillis))
	if err != nil {
		t.Fatal(err)
	}
	dq, _ := url.Parse(dingURL)
	if dq.Query().Get("sign") == feishuSign(ts, signTestSecret) {
		t.Fatal("DingTalk and Feishu signatures match; one algorithm is wrong")
	}
}

func TestDingTalkNoSecretLeavesURLUntouched(t *testing.T) {
	// Unsigned robots get no invented timestamp/sign fields.
	const hook = "https://oapi.dingtalk.com/robot/send?access_token=tok"
	got, err := dingTalkSignedURL(hook, "", time.UnixMilli(signTestTSMillis))
	if err != nil {
		t.Fatal(err)
	}
	if got != hook {
		t.Fatalf("unsigned URL should be unchanged, got %q", got)
	}
}

func TestValidateHTTPURL(t *testing.T) {
	ok := []string{"https://example.com/hook", "http://10.0.0.1:8080/x?y=1"}
	for _, s := range ok {
		if err := validateHTTPURL(s); err != nil {
			t.Errorf("%q should be accepted: %v", s, err)
		}
	}
	// Reject unsupported schemes such as file.
	bad := []string{"", "file:///etc/passwd", "ftp://example.com", "https://", "gopher://x"}
	for _, s := range bad {
		if err := validateHTTPURL(s); err == nil {
			t.Errorf("%q should be rejected", s)
		}
	}
}

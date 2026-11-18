package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Use ASCII fixture data so any Han characters in output identify untranslated labels rather than original evidence.
func asciiStatusChangeItem() Item {
	return Item{
		FindingID:  7,
		Name:       "login-flaw",
		VulnClass:  "SQLi",
		Severity:   "high",
		Summary:    "SQL injection via q param",
		Assets:     []string{"a.example.com"},
		DetailURL:  "https://platform.example/finding/7",
		FromStatus: "pending",
		ToStatus:   "fixed",
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("serialize card: %v", err)
	}
	return string(b)
}

// Test Feishu item labels independently of shared markdown renderers, requiring consistent English wording.
func TestFeishuItemLinesLocalized(t *testing.T) {
	got := feishuItemLines(asciiStatusChangeItem())
	assertEnglish(t, "feishuItemLines", got)
	for _, want := range []string{
		"**Status changed**: Pending → Fixed",
		"**Type**: SQLi",
		"**Assets**: a.example.com",
		"**Overview**: SQL injection via q param",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Feishu single body lacks %q:\n%s", want, got)
		}
	}
}

// Test English Feishu card buttons and Han-free output with ASCII fixtures.
func TestFeishuCardButtonsLocalized(t *testing.T) {
	// Single-item detail button.
	single, _ := feishuCard(Message{Items: []Item{asciiStatusChangeItem()}})
	singleJSON := mustJSON(t, single)
	if hasHan(singleJSON) {
		t.Errorf("Feishu single card has untranslated Han text:\n%s", singleJSON)
	}
	if !strings.Contains(singleJSON, "View details") {
		t.Errorf("Feishu single card lacks View details:\n%s", singleJSON)
	}

	// Digest dashboard button.
	batch, _ := feishuCard(Message{Batch: true, HomeURL: "https://platform.example", Items: hanFreeItems(2)})
	batchJSON := mustJSON(t, batch)
	if hasHan(batchJSON) {
		t.Errorf("Feishu digest card has untranslated Han text:\n%s", batchJSON)
	}
	if !strings.Contains(batchJSON, "View all in ARTEX") {
		t.Errorf("Feishu digest card lacks View all in ARTEX:\n%s", batchJSON)
	}
}

// Test the DingTalk detail button through Send against a synthetic receiver.
func TestDingTalkActionCardButtonLocalized(t *testing.T) {
	var singleTitle string
	srv := capturePost(t, `{"errcode":0,"errmsg":"ok"}`, func(_ *testing.T, body map[string]any, _ *http.Request) {
		card, _ := body["actionCard"].(map[string]any)
		singleTitle, _ = card["singleTitle"].(string)
	})
	m := Message{Items: []Item{asciiStatusChangeItem()}}
	if _, err := (dingTalkChannel{}).Send(context.Background(), map[string]any{"webhook": srv.URL}, m); err != nil {
		t.Fatalf("delivery failed: %v", err)
	}
	if singleTitle != "View details" {
		t.Errorf("DingTalk singleTitle should be View details, got %q", singleTitle)
	}
}

// Validate English DingTalk/Feishu/WeCom errors while retaining the Webhook technical term.
func TestChinaPlatformValidateLocalized(t *testing.T) {
	for _, kind := range []string{KindDingTalk, KindFeishu, KindWeCom} {
		ch, ok := Get(kind)
		if !ok {
			t.Fatalf("channel %s is not registered", kind)
		}

		// Empty configuration requires a webhook URL.
		missing := ch.Validate(map[string]any{})
		if missing == nil {
			t.Fatalf("%s: empty config should fail", kind)
		}
		assertEnglish(t, kind+" missing", missing.Error())
		if !strings.Contains(missing.Error(), "Webhook") || !strings.Contains(missing.Error(), "required") {
			t.Errorf("%s: expected Webhook URL is required, got %q", kind, missing)
		}

		// An unsupported FTP address returns an invalid-webhook explanation.
		bad := ch.Validate(map[string]any{"webhook": "ftp://x"})
		if bad == nil {
			t.Fatalf("%s: FTP URL should fail validation", kind)
		}
		if hasHan(bad.Error()) {
			t.Errorf("%s: invalid-URL error contains Han text: %q", kind, bad)
		}
		if !strings.Contains(bad.Error(), "Invalid webhook URL") {
			t.Errorf("%s: expected Invalid webhook URL prefix, got %q", kind, bad)
		}
	}
}

// Test English application-failure messages stored in delivery history. ASCII peer errors ensure Han characters identify untranslated framing.
func TestChinaPlatformSendErrorsLocalized(t *testing.T) {
	// HTTP 200 with a nonzero platform application code.
	bizCases := []struct {
		kind     string
		resp     string
		wantSubs []string
	}{
		{KindDingTalk, `{"errcode":310000,"errmsg":"keyword not matched"}`, []string{"DingTalk error", "310000"}},
		{KindFeishu, `{"code":19021,"msg":"sign error"}`, []string{"Feishu error", "19021"}},
		{KindWeCom, `{"errcode":45009,"errmsg":"freq out of limit"}`, []string{"WeCom request throttled", "45009"}},
		{KindWeCom, `{"errcode":93000,"errmsg":"invalid webhook"}`, []string{"WeCom error", "93000"}},
	}
	for _, tc := range bizCases {
		srv := capturePost(t, tc.resp, nil)
		ch, ok := Get(tc.kind)
		if !ok {
			t.Fatalf("channel %s is not registered", tc.kind)
		}
		_, err := ch.Send(context.Background(), map[string]any{"webhook": srv.URL}, singleMsg())
		if err == nil {
			t.Fatalf("%s: response %s should fail", tc.kind, tc.resp)
		}
		if hasHan(err.Error()) {
			t.Errorf("%s: send error contains Han text: %q", tc.kind, err)
		}
		for _, sub := range tc.wantSubs {
			if !strings.Contains(err.Error(), sub) {
				t.Errorf("%s: send error lacks %q, got %q", tc.kind, sub, err)
			}
		}
	}

	// A non-JSON response produces an English parsing failure.
	parseCases := []struct {
		kind     string
		platform string
	}{
		{KindDingTalk, "DingTalk"},
		{KindFeishu, "Feishu"},
		{KindWeCom, "WeCom"},
	}
	for _, pc := range parseCases {
		srv := capturePost(t, `not-json`, nil)
		ch, ok := Get(pc.kind)
		if !ok {
			t.Fatalf("channel %s is not registered", pc.kind)
		}
		_, err := ch.Send(context.Background(), map[string]any{"webhook": srv.URL}, singleMsg())
		if err == nil {
			t.Fatalf("%s: non-JSON response should fail", pc.kind)
		}
		want := "Could not parse " + pc.platform + " response"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: parse error lacks %q, got %q", pc.kind, want, err)
		}
	}
}

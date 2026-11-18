package notify

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Pin the user-template capability boundary: adding exported methods or functions could silently grant file/network/command access.

// Template context types expose only data fields, with no exported methods callable by text/template. Any new method requires an explicit capability review.
func TestTemplateContextHasNoMethods(t *testing.T) {
	for _, v := range []any{webhookTemplateData{}, webhookItem{}} {
		typ := reflect.TypeOf(v)
		if n := typ.NumMethod(); n != 0 {
			var names []string
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Fatalf("%s exposes %d methods (%s) callable by text/template, granting their capabilities to template authors", typ.Name(), n, strings.Join(names, ", "))
		}
	}
}

// Only json/jsons serialization helpers are available; no file, network or command functions.
func TestTemplateFuncsAreMinimal(t *testing.T) {
	var got []string
	for name := range webhookTemplateFuncs {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"json", "jsons"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("template function set changed: got %v, want %v; new functions must not grant filesystem, network or command capabilities", got, want)
	}
}

// Unknown-data access fails without exposing internal context values.
func TestTemplateCannotReachUnknownData(t *testing.T) {
	_, err := renderWebhookBody(`{"x": {{.Environment}}, "y": {{.Env}}}`, singleMsg())
	if err == nil {
		t.Fatal("unknown template field should fail")
	}
	// Errors do not include actual finding title/summary content.
	for _, leak := range []string{"SQL注入", "参数 id"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("template error leaked message content %q: %v", leak, err)
		}
	}
}

// Invalid templates are permanent configuration failures, not repeated transient retries.
func TestTemplateRenderFailsPermanently(t *testing.T) {
	cfg := map[string]any{
		"url":           "https://example.com/hook",
		"body_template": `{{.Items.`,
	}
	if err := (webhookChannel{}).Validate(cfg); err == nil {
		t.Fatal("invalid template syntax should fail save validation")
	}
	// Direct sending must enforce the same permanent failure even if save-time validation is bypassed.
	_, err := (webhookChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("bad template should be permanent, got %v", err)
	}
}

// Rendered payloads must be valid JSON rather than arbitrary text for another protocol.
func TestTemplateCanOnlyProduceJSON(t *testing.T) {
	// Accept a valid template.
	ok := map[string]any{"url": "https://example.com/hook", "body_template": `{"t":{{json .Title}}}`}
	if err := (webhookChannel{}).Validate(ok); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	// Reject non-JSON output rather than forwarding it verbatim.
	bad := map[string]any{"url": "http://127.0.0.1:1/hook", "body_template": `not json {{.Count}}`}
	_, err := (webhookChannel{}).Send(context.Background(), bad, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("non-JSON output should be permanent, got %v", err)
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("error should identify invalid JSON, got %v", err)
	}
}

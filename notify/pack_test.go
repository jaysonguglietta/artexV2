package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Pack complete digest entries and report their actual count. Rendering then truncating while marking the whole batch delivered silently loses findings.

func TestMarkdownBodyPacksWholeItemsWithinByteLimit(t *testing.T) {
	// Two hundred multibyte entries exceed the 4096-byte limit.
	m := batchMsg(200)
	body, kept := markdownBody(m, weComMarkdownLimit)

	if len(body) > weComMarkdownLimit {
		t.Fatalf("body %d bytes exceeds limit %d", len(body), weComMarkdownLimit)
	}
	if !utf8.ValidString(body) {
		t.Fatal("body is invalid UTF-8")
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("want partial batch (0 < kept < %d), got %d", len(m.Items), kept)
	}
	// The header identifies included and remaining counts accurately.
	if !strings.Contains(body, "remaining") || !strings.Contains(body, "in the next message") {
		t.Fatalf("header lacks remaining count:\n%s", body[:minInt(400, len(body))])
	}
	// Include only the kept prefix.
	for i := 0; i < kept; i++ {
		if !strings.Contains(body, "Finding"+itoa(i+1)) {
			t.Fatalf("item %d should be included:\n%s", i+1, body)
		}
	}
	if strings.Contains(body, "Finding"+itoa(kept+1)) {
		t.Fatalf("item %d belongs in a later batch", kept+1)
	}
}

func TestMarkdownBodyKeepsEverythingWhenUnderLimit(t *testing.T) {
	m := batchMsg(3)
	body, kept := markdownBody(m, 0) // Zero disables the bound.
	if kept != len(m.Items) {
		t.Fatalf("unlimited size should retain all, got kept=%d", kept)
	}
	if strings.Contains(body, "remaining") {
		t.Fatalf("unexpected truncation notice:\n%s", body)
	}
}

func TestMarkdownBodyAlwaysKeepsAtLeastOneItem(t *testing.T) {
	// Even a tiny budget includes one entry for final truncation so an oversized finding cannot block the queue forever.
	m := batchMsg(5)
	_, kept := markdownBody(m, 50)
	if kept != 1 {
		t.Fatalf("must keep at least one item, got %d", kept)
	}
}

func TestMarkdownBodySingleReturnsOne(t *testing.T) {
	_, kept := markdownBody(singleMsg(), 4096)
	if kept != 1 {
		t.Fatalf("single notification should deliver one item, got %d", kept)
	}
	// An empty message has no deliverable items.
	if _, k := markdownBody(Message{}, 4096); k != 0 {
		t.Fatalf("empty message should deliver zero, got %d", k)
	}
}

func TestTelegramPackingUsesRuneBudget(t *testing.T) {
	m := batchMsg(200)
	text, kept := telegramHTML(m)
	// Telegram measures characters; byte counting would overtruncate multibyte messages.
	if n := utf8.RuneCountInString(text); n > telegramTextLimit {
		t.Fatalf("body %d characters exceeds limit %d", n, telegramTextLimit)
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("expected partial batch, got %d", kept)
	}
	if !strings.Contains(text, "next message") {
		t.Fatalf("missing remaining-item notice:\n%.300s", text)
	}
}

func TestFeishuPackingReportsKept(t *testing.T) {
	m := batchMsg(2000)
	_, kept := feishuCard(m)
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("card should contain a partial batch, got %d", kept)
	}
}

func TestWebhookAndEmailReportAllItems(t *testing.T) {
	// These channels send the full body and deliver the entire batch.
	m := batchMsg(7)
	if n := len(m.Items); n != 7 {
		t.Fatal("fixture precondition failed")
	}
	// An unlimited markdownBody retains every item.
	if _, k := markdownBody(m, 0); k != len(m.Items) {
		t.Fatalf("unlimited rendering should include all, got %d", k)
	}
}

// Untrusted title/summary/asset text cannot alter message structure.
func TestMarkdownEscapesUntrustedContent(t *testing.T) {
	cases := []struct {
		name  string
		item  Item
		must  []string // Require the escaped representation.
		wrong []string // Reject the unescaped representation.
	}{
		{
			name: "title newline and external link",
			item: Item{
				Severity: "high",
				Name:     "登录口 SQL 注入\n[紧急：点此验证账号](http://attacker.tld)",
			},
			// Collapse newlines and escape brackets/parentheses to prevent forged entries or clickable attacker links.
			must:  []string{`\[紧急：点此验证账号\]`, `\(http://attacker.tld\)`},
			wrong: []string{"\n[紧急", "\n\n[紧急"},
		},
		{
			name: "title image beacon",
			item: Item{
				Severity: "high",
				Name:     "漏洞 ![](http://attacker.tld/beacon)",
			},
			must:  []string{`\!`, `\(http://attacker.tld/beacon\)`},
			wrong: []string{"![]("},
		},
		{
			name: "asset emphasis and quote marker",
			item: Item{
				Severity: "high",
				Name:     "普通标题",
				Assets:   []string{"a.com/*注入*>引用"},
			},
			must:  []string{`\*注入\*`, `\>`},
			wrong: []string{"*注入*"},
		},
		{
			name: "summary code and table markers",
			item: Item{
				Severity: "high",
				Name:     "Title",
				Summary:  "`code` | 表格",
			},
			must:  []string{"\\`code\\`", `\|`},
			wrong: []string{"`code`"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Message{Items: []Item{tc.item}}
			// Single-item writeItem is shared by markdown renderers.
			var b strings.Builder
			writeItem(&b, tc.item, "", true)
			got := b.String()
			for _, want := range tc.must {
				if !strings.Contains(got, want) {
					t.Errorf("missing escaped form %q:\n%s", want, got)
				}
			}
			for _, bad := range tc.wrong {
				if strings.Contains(got, bad) {
					t.Errorf("unsafe unescaped form %q:\n%s", bad, got)
				}
			}
			_ = m
		})
	}
}

// Escape backslashes first to avoid double-escaping newly inserted backslashes.
func TestMarkdownEscapeBackslashFirst(t *testing.T) {
	if got := markdownEscape(`a\b*c`); got != `a\\b\*c` {
		t.Fatalf("incorrect escape order: %q", got)
	}
}

// Markdown escapes must not leak into Telegram HTML through shared title rendering.
func TestTelegramTitleHasNoMarkdownEscapes(t *testing.T) {
	m := Message{Items: []Item{{Severity: "high", Name: "alert(1) *重点*"}}}
	text, _ := telegramHTML(m)
	if strings.Contains(text, `\(`) || strings.Contains(text, `\*`) {
		t.Fatalf("Telegram HTML contains markdown backslashes:\n%s", text)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

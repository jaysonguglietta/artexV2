package notify

import (
	"strings"
	"testing"
)

// ASCII item fixtures make an output-wide Han check detect translated-label regressions rather than evidence content.
func hanFreeItems(n int) []Item {
	items := make([]Item, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, Item{
			FindingID: int64(i + 1),
			Name:      "login-flaw",
			VulnClass: "SQLi",
			Severity:  "high",
			Summary:   "SQL injection via q param",
			Assets:    []string{"a.example.com"},
		})
	}
	return items
}

// Test the English shared message title across markdown, card, HTML, Telegram and webhook contexts.
func TestMarkdownTitleLocalized(t *testing.T) {
	// Digest title and total count.
	got := markdownTitle(Message{Batch: true, Items: hanFreeItems(3)})
	assertEnglish(t, "markdownTitle(batch)", got)
	if !strings.Contains(got, "Finding summary") || !strings.Contains(got, "total 3") {
		t.Errorf("digest title should be Finding summary - total 3, got %q", got)
	}
	// Empty-item notification title.
	empty := markdownTitle(Message{})
	assertEnglish(t, "markdownTitle(empty)", empty)
	if empty != "Finding notification" {
		t.Errorf("empty message title should be Finding notification, got %q", empty)
	}
}

// Test English digest time/count/continuation framing using Han-free data.
func TestMarkdownBatchIntroLocalized(t *testing.T) {
	items := hanFreeItems(3)

	// Introduction with a time window.
	win := markdownBatchIntro(Message{Batch: true, WindowMinutes: 30}, items, 3)
	if hasHan(win) {
		t.Errorf("window introduction contains Han text: %q", win)
	}
	if !strings.Contains(win, "Last 30 minutes") || !strings.Contains(win, "New findings: 3") {
		t.Errorf("window introduction should report Last 30 minutes New findings: 3, got %q", win)
	}

	// Introduction without a time window.
	noWin := markdownBatchIntro(Message{Batch: true}, items, 3)
	if hasHan(noWin) {
		t.Errorf("introduction contains Han text: %q", noWin)
	}
	if !strings.Contains(noWin, "New findings: 3") || strings.Contains(noWin, "minutes") {
		t.Errorf("windowless introduction should report New findings: 3, got %q", noWin)
	}

	// Partial batches identify included and remaining entries.
	trunc := markdownBatchIntro(Message{Batch: true, WindowMinutes: 30}, items, 5)
	if hasHan(trunc) {
		t.Errorf("continuation notice contains Han text: %q", trunc)
	}
	for _, want := range []string{"Showing first 3", "remaining 2", "in the next message"} {
		if !strings.Contains(trunc, want) {
			t.Errorf("continuation notice lacks %q, got %q", want, trunc)
		}
	}
}

// Test English single-finding labels and detail link in the shared markdown item renderer.
func TestWriteItemLabelsLocalized(t *testing.T) {
	it := Item{
		Name:       "login-flaw",
		VulnClass:  "SQLi",
		Severity:   "high",
		Summary:    "SQL injection via q param",
		Assets:     []string{"a.example.com"},
		DetailURL:  "https://platform.example/finding/1",
		FromStatus: "pending",
		ToStatus:   "fixed",
	}
	var b strings.Builder
	writeItem(&b, it, "", true)
	got := b.String()

	assertEnglish(t, "writeItem(single)", got)
	for _, want := range []string{
		"**Status changed**: Pending → Fixed",
		"**Type**: SQLi",
		"**Assets**: a.example.com",
		"**Overview**: SQL injection via q param",
		"[View details](https://platform.example/finding/1)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("single rendering lacks %q:\n%s", want, got)
		}
	}
}

// Test the English dashboard footer link when HomeURL is configured.
func TestMarkdownBodyFooterLocalized(t *testing.T) {
	m := Message{Batch: true, HomeURL: "https://platform.example", Items: hanFreeItems(2)}
	body, kept := markdownBody(m, 0)
	if kept != 2 {
		t.Fatalf("unlimited rendering should include both, got %d", kept)
	}
	if !strings.Contains(body, "[View all in ARTEX](https://platform.example)") {
		t.Errorf("body lacks View all in ARTEX footer:\n%s", body)
	}
}

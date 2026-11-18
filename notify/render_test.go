package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateBytesKeepsValidUTF8(t *testing.T) {
	// Test UTF-8 boundaries across mixed multibyte inputs and every cutoff, because byte-based platform limits cannot split runes.
	inputs := []string{
		"中文测试内容",
		"混合 mixed 内容 content",
		"a中b文c测d试e",
		"🔴🟠🟡🔵", // Four-byte emoji makes boundary mistakes obvious.
		strings.Repeat("Finding", 100),
	}
	for _, in := range inputs {
		for max := 1; max <= len(in)+2; max++ {
			got := TruncateBytes(in, max)
			if !utf8.ValidString(got) {
				t.Fatalf("input %q max=%d: invalid UTF-8 %q", in, max, got)
			}
			if len(got) > max {
				t.Fatalf("input %q max=%d: result %d bytes exceeds limit", in, max, len(got))
			}
			// Do not alter untruncated content.
			if len(in) <= max && got != in {
				t.Fatalf("input %q max=%d: untruncated content changed to %q", in, max, got)
			}
		}
	}
}

func TestTruncateBytesZeroMeansUnlimited(t *testing.T) {
	long := strings.Repeat("x", 10000)
	if got := TruncateBytes(long, 0); got != long {
		t.Fatal("max=0 must be unlimited")
	}
	if got := TruncateBytes(long, -5); got != long {
		t.Fatal("negative max must be unlimited")
	}
}

func TestTruncateBytesEllipsisBudget(t *testing.T) {
	// An ellipsis must not itself exceed a tiny budget.
	got := TruncateBytes("abcdefgh", 1)
	if len(got) > 1 {
		t.Fatalf("max=1: result %q has %d bytes", got, len(got))
	}
	// Normal truncation adds an ellipsis.
	if got := TruncateBytes("abcdefgh", 5); !strings.HasSuffix(got, ellipsis) {
		t.Fatalf("expected ellipsis, got %q", got)
	}
}

func TestTruncateRunesCountsCharactersNotBytes(t *testing.T) {
	// Keep character-based Telegram limits distinct from byte-based WeCom limits.
	s := "一二三四五六七八九十"
	got := TruncateRunes(s, 5)
	if n := utf8.RuneCountInString(got); n != 5 {
		t.Fatalf("want five characters, got %d (%q)", n, got)
	}
	// The same multibyte string is substantially shorter under a byte budget.
	if utf8.RuneCountInString(TruncateBytes(s, 5)) >= 5 {
		t.Fatal("byte limit should differ from rune-count limit")
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	got := OneLine("第一行\n\n第二行\t带制表   多空格", 0)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("whitespace not collapsed: %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("consecutive spaces remain: %q", got)
	}
	// Truncated output remains readable and valid.
	got = OneLine("一二三四五六七八九十", 4)
	if n := utf8.RuneCountInString(got); n != 4 {
		t.Fatalf("want four characters, got %d (%q)", n, got)
	}
}

func TestTruncateHTMLNeverCutsTagInHalf(t *testing.T) {
	// HTML truncation must not leave a partial tag rejected by the receiver.
	s := `<b>标题</b>正文正文正文<a href="https://example.com/very/long/path">查看详情</a>`
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		if n := utf8.RuneCountInString(got); max > 0 && n > max {
			t.Fatalf("max=%d: result %d characters exceeds limit", max, n)
		}
		// No unmatched trailing less-than sign.
		if lt := strings.LastIndex(got, "<"); lt >= 0 && !strings.Contains(got[lt:], ">") {
			t.Fatalf("max=%d: partial trailing tag %q", max, got)
		}
	}
}

func TestAssetLineOmitsExcess(t *testing.T) {
	if got := assetLine(nil, 3); got != "" {
		t.Fatalf("no assets should return empty, got %q", got)
	}
	if got := assetLine([]string{"a", "b"}, 3); got != "a, b" {
		t.Fatalf("within-limit list should retain all, got %q", got)
	}
	// Over-limit asset lists report their total count.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	if !strings.Contains(got, "(5 total)") {
		t.Fatalf("missing total five: %q", got)
	}
}

func TestSeverityAndStatusLabels(t *testing.T) {
	if AtLeast("", "low") {
		t.Fatal("empty severity rank zero should fail a threshold")
	}
	if !AtLeast("critical", "") {
		t.Fatal("empty threshold should allow")
	}
	if got := StatusLabel("fixed"); got != "Fixed" {
		t.Fatalf("unknown status mapping: %q", got)
	}
	// Unknown status identifiers pass through unchanged.
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Fatalf("unknown status should pass through, got %q", got)
	}
}

// Avoid partial HTML entities as well as partial tags; otherwise common oversized digests can be rejected entirely.
func TestTruncateHTMLNeverCutsEntity(t *testing.T) {
	s := "aaaa&amp;bbbb&lt;cccc&quot;dddd"
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		// No trailing entity fragment without a semicolon.
		if amp := strings.LastIndex(got, "&"); amp >= 0 && !strings.Contains(got[amp:], ";") {
			t.Fatalf("max=%d: incomplete trailing entity %q", max, got[amp:])
		}
		if strings.Contains(got, "&amp\x00") {
			t.Fatalf("max=%d: malformed entity", max)
		}
	}
}

package notify

import (
	"fmt"
	"strings"
)

// Shared markdown rendering for DingTalk and WeCom. Feishu uses card JSON; Telegram and email use their own HTML renderers.

// maxAssetsShown limits low-value asset-list expansion in compact IM notifications.
const maxAssetsShown = 3

// maxSummaryRunes keeps summaries compact; notifications point readers to the complete report rather than duplicating it.
const maxSummaryRunes = 120

// markdownReservedBytes protects the header, severity distribution, continuation notice and footer link from item packing/truncation so the batch and hidden-item count remain visible.
const markdownReservedBytes = 320

// markdownEscape encodes structural markdown characters in untrusted model/target titles, summaries, classes and asset URLs. Otherwise content can inject clickable phishing links, remote tracking images or formatting that hides findings. Escape backslashes first so newly inserted escape characters are not escaped twice.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"!", `\!`,
		"#", `\#`,
		">", `\>`,
		"|", `\|`,
		"~", `\~`,
	)
	return replacer.Replace(s)
}

// markdownText collapses untrusted text to one line and escapes it. Escaping alone cannot prevent newlines from forging extra list entries or quote blocks.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns unescaped display text shared across markdown, HTML, plain_text, JSON and subject contexts. Each final renderer must apply its own encoding; shared markdown escaping would produce visible backslashes or corrupt other output formats.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf("Finding summary - total %d", len(m.Items))
	}
	if len(m.Items) == 0 {
		return "Finding notification"
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity), OneLine(it.Title(), 0))
}

// markdownBody returns content and the number of complete items actually included. Only that prefix may be marked delivered; size-excluded items remain queued. Nonpositive maxBytes disables the bound.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true)
		// Always include a single oversized item and let final truncation preserve a useful partial notification rather than sending nothing.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf("\n[View all in ARTEX](%s)\n", m.HomeURL)
	}
	kept := packItemCount(m.Items, maxBytes, markdownReservedBytes, footer, byteSize, func(it Item, idx int) string {
		var b strings.Builder
		writeItem(&b, it, fmt.Sprintf("%d. ", idx+1), false)
		return b.String()
	})

	items := m.Items[:kept]
	var b strings.Builder
	b.WriteString(markdownBatchIntro(m, items, len(m.Items)))
	for i, it := range items {
		writeItem(&b, it, fmt.Sprintf("%d. ", i+1), false)
	}
	b.WriteString(footer)
	return TruncateBytes(b.String(), maxBytes), kept
}

// markdownBatchIntro summarizes the window, included count and severity distribution. If items is smaller than total, explicitly identify entries deferred to the next message so they are not silently hidden.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, "**Last %d minutes: New findings: %d**", m.WindowMinutes, total)
	} else {
		fmt.Fprintf(&b, "**New findings: %d**", total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, "(Showing first %d; remaining %d continue in the next message)", len(items), extra)
	}
	// Count severities only among entries actually present in this message.
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", SeverityLabel(sev), n))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	b.WriteString("\n\n")
	return b.String()
}

// writeItem renders a full single finding with summary/backlink, or a compact numbered digest row. All external fields pass through markdownText. The operator-configured public_base_url backlink remains clickable and is handled as a trusted configured link.
func writeItem(b *strings.Builder, it Item, prefix string, single bool) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if !single {
		// Digest entries keep asset and summary details on one compact line.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " — " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, "**Status changed**: %s → %s\n",
			markdownText(StatusLabel(it.FromStatus), 0), markdownText(StatusLabel(it.ToStatus), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		fmt.Fprintf(b, "**Type**: %s\n", markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		fmt.Fprintf(b, "**Assets**: %s\n", markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			fmt.Fprintf(b, "**Overview**: %s\n", s)
		}
	}
	if it.DetailURL != "" {
		fmt.Fprintf(b, "[View details](%s)\n", it.DetailURL)
	}
}

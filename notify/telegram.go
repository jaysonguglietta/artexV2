package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Telegram sendMessage limits the text field by character count.
const telegramTextLimit = 4096

// Telegram authenticates in /bot<token>/sendMessage, without signing. Use HTML instead of MarkdownV2 to reduce required escaping; still inspect the application-level ok field even after HTTP 200.
type telegramChannel struct{}

func (telegramChannel) Kind() string { return KindTelegram }

// Use a conservative rate reflecting approximately one private-chat message/second and 20 group messages/minute.
func (telegramChannel) DefaultRatePerMin() int { return 20 }

// The bot token is the credential; chat_id identifies the recipient but cannot authenticate a send.
func (telegramChannel) SecretKeys() []string { return []string{"bot_token"} }

// base_url controls which endpoint receives the token; changing it requires an explicit token decision.
func (telegramChannel) DestinationKeys() []string { return []string{"base_url"} }

func (telegramChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "bot_token") == "" {
		return errors.New("Bot Token is required")
	}
	if cfgString(cfg, "chat_id") == "" {
		return errors.New("Chat ID is required")
	}
	if base := cfgString(cfg, "base_url"); base != "" {
		if err := validateHTTPURL(base); err != nil {
			return fmt.Errorf("Invalid API URL: %w", err)
		}
	}
	return nil
}

func (c telegramChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := telegramEndpoint(cfg)
	if err != nil {
		return 0, Permanent(err)
	}
	text, kept := telegramHTML(m)
	payload := map[string]any{
		"chat_id":                  cfgString(cfg, "chat_id"),
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": false,
	}
	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Could not parse Telegram response: %w (%s)", err, snippet(raw))
	}
	if res.OK {
		return kept, nil
	}
	// 429 is transient rate limiting. Other listed 400/401/403/404 responses are configuration failures that cannot heal by retrying.
	if res.ErrorCode == 429 {
		return 0, fmt.Errorf("Telegram request throttled: %s", res.Description)
	}
	return 0, Permanent(fmt.Errorf("Telegram response error %d: %s", res.ErrorCode, res.Description))
}

// TelegramEndpoint uses the official API by default and an explicit configured base URL for an operator-owned Bot API reverse proxy.
func telegramEndpoint(cfg map[string]any) (string, error) {
	base := cfgString(cfg, "base_url")
	if base == "" {
		base = "https://api.telegram.org"
	}
	base = strings.TrimSuffix(base, "/")
	token := cfgString(cfg, "bot_token")
	raw := base + "/bot" + token + "/sendMessage"
	u, err := url.Parse(raw)
	if err != nil {
		// Do not return parser errors or the address because the path contains the bot token.
		return "", fmt.Errorf("Could not construct API URL (base: %s)", redactRequestTarget(base))
	}
	return u.String(), nil
}

// TelegramHTML returns rendered content and the number of items actually included.
func telegramHTML(m Message) (string, int) {
	var b strings.Builder
	b.WriteString("<b>" + telegramEscape(markdownTitle(m)) + "</b>\n")
	if m.Batch {
		// Pack using runeSize because Telegram measures characters, not bytes.
		footer := ""
		if m.HomeURL != "" {
			footer = fmt.Sprintf("\n\n<a href=\"%s\">View all in ARTEX</a>", telegramEscapeAttr(m.HomeURL))
		}
		kept := packItemCount(m.Items, telegramTextLimit, telegramReservedRunes, footer, runeSize, func(it Item, idx int) string {
			return telegramBatchLine(it, idx+1)
		})
		items := m.Items[:kept]
		b.Reset()
		b.WriteString("<b>" + telegramEscape(telegramBatchTitle(m, items, len(m.Items))) + "</b>")
		for i, it := range items {
			b.WriteString("\n" + telegramEscape(telegramBatchLine(it, i+1)))
		}
		b.WriteString(footer)
		return TruncateHTML(b.String(), telegramTextLimit), kept
	}
	if len(m.Items) == 0 {
		return b.String(), 0
	}
	it := m.Items[0]
	if it.IsStatusChange() {
		b.WriteString(fmt.Sprintf("\n<b>Status changed</b>: %s → %s",
			telegramEscape(StatusLabel(it.FromStatus)), telegramEscape(StatusLabel(it.ToStatus))))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		b.WriteString("\n<b>Type</b>: " + telegramEscape(it.VulnClass))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		b.WriteString("\n<b>Assets</b>: " + telegramEscape(a))
	}
	if s := OneLine(it.Summary, maxSummaryRunes); s != "" {
		b.WriteString("\n<b>Overview</b>: " + telegramEscape(s))
	}
	if it.DetailURL != "" {
		b.WriteString(fmt.Sprintf("\n\n<a href=\"%s\">View details</a>", telegramEscapeAttr(it.DetailURL)))
	}
	return TruncateHTML(b.String(), telegramTextLimit), 1
}

// Reserve character capacity for the title and any continuation notice.
const telegramReservedRunes = 160

// TelegramBatchLine formats one unescaped digest entry; the caller escapes the complete result.
func telegramBatchLine(it Item, idx int) string {
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		return fmt.Sprintf("%d. %s · %s — %s", idx, SeverityLabel(it.Severity), it.Title(), a)
	}
	return fmt.Sprintf("%d. %s · %s", idx, SeverityLabel(it.Severity), it.Title())
}

// TelegramBatchTitle reports the number actually included in this message rather than implying the entire batch fit.
func telegramBatchTitle(m Message, items []Item, total int) string {
	title := fmt.Sprintf("Finding summary - total %d", total)
	if extra := total - len(items); extra > 0 {
		title += fmt.Sprintf(" (showing first %d; remaining %d continue in the next message)", len(items), extra)
	}
	if m.WindowMinutes > 0 {
		title = fmt.Sprintf("Last %d minutes - %s", m.WindowMinutes, title)
	}
	return title
}

// TelegramEscape encodes HTML text. Double-encoding existing entities is intentional because untrusted input must be displayed literally, not interpreted as HTML.
func telegramEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// TelegramEscapeAttr also escapes quotes so URLs cannot terminate href attributes.
func telegramEscapeAttr(s string) string {
	s = telegramEscape(s)
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

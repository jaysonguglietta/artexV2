package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// WeCom markdown has a hard byte limit, the tightest among the adapters and the primary reason for TruncateBytes.
const weComMarkdownLimit = 4096

// WeCom authenticates solely through the webhook key. It does not support signing. Its 4096-byte markdown limit rejects the whole oversized message, so pack/truncate locally and enforce the 20/minute rate.
type weComChannel struct{}

func (weComChannel) Kind() string { return KindWeCom }

func (weComChannel) DefaultRatePerMin() int { return 20 }

// The complete webhook URL is the only credential and is masked as a unit.
func (weComChannel) SecretKeys() []string { return []string{"webhook"} }

// The webhook is both destination and complete credential, so changing it leaves no separate old secret to leak.
func (weComChannel) DestinationKeys() []string { return []string{"webhook"} }

func (weComChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Webhook URL is required")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Invalid webhook URL: %w", err)
	}
	return nil
}

func (c weComChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	// Pack/truncate locally rather than allowing the platform to reject an entire large digest; preserve the remaining entries for later messages.
	content, kept := markdownBody(m, weComMarkdownLimit)
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	}
	raw, err := doJSON(ctx, "POST", cfgString(cfg, "webhook"), nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Could not parse WeCom response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 45009 is transient platform rate limiting. Backoff is a fallback; lower the configured rate to fix repeated excess calls.
		if res.ErrCode == 45009 {
			return 0, fmt.Errorf("WeCom request throttled (%d): %s", res.ErrCode, res.ErrMsg)
		}
		// 93000 means an invalid webhook key and is a permanent failure.
		return 0, Permanent(fmt.Errorf("WeCom error (%d): %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

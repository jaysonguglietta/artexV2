package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements DingTalk custom robots. Enforce its 20/minute limit locally because excess messages can be silently discarded despite HTTP 200. Support signing or an unsigned webhook, rather than content-dependent keyword protection. Always inspect errcode in successful HTTP responses.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// The webhook URL contains access_token and is masked as a complete credential.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// The webhook is the destination; changing it requires explicitly replacing or clearing the signing secret.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Webhook URL is required")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Invalid webhook URL: %w", err)
	}
	return nil
}

// Send uses an ActionCard for a single item with a detail link; otherwise it uses markdown.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// Bound markdown defensively even without a documented platform byte limit, since evidence fields may grow unexpectedly.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    "View details",
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk reports application failures inside HTTP 200 responses.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Could not parse DingTalk response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 is a signature failure and 310000 a keyword mismatch. These configuration failures cannot heal through retries.
		return 0, Permanent(fmt.Errorf("DingTalk error (%d): %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL appends timestamp and sign. Sign timestamp + newline + secret with HMAC-SHA256 using secret as the key, base64-encode and URL-encode the result. Timestamps are milliseconds; an empty secret preserves unsigned webhook behavior.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Do not return the parse error: it embeds the complete credential-bearing URL.
		return "", fmt.Errorf("Could not parse webhook URL: %s", redactRequestTarget(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL restricts schemes and rejects forbidden literal IPs early. Redact parser errors because webhook URLs may contain tokens. Hostnames are checked again at dial time by blockInternalDial, covering DNS rebinding. This early literal check provides immediate configuration feedback.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("Could not resolve address (%s)", redactRequestTarget(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Only HTTP and HTTPS are supported; received %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("Host name is required")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return fmt.Errorf("Local/link-local destination %s is blocked (set %s=1 only if local delivery is intended)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

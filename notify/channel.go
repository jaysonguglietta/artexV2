package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel adapts one delivery service. Implementations must be stateless because multiple configurations share an instance concurrently; credentials always come from cfg.
type Channel interface {
	// Kind returns the registered channel identifier.
	Kind() string
	// Validate checks required fields and formats when saving configuration. Its user-facing errors should identify the missing field rather than merely saying configuration is invalid.
	Validate(cfg map[string]any) error
	// Send returns the number of items actually delivered and an error. Channel size limits may exclude a suffix of a digest: mark only the first kept items delivered and retain the rest for a later batch. On error, ignore kept; PermanentError identifies failures that should not be retried.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin supplies the service-recommended initial rate; zero means no known limit.
	DefaultRatePerMin() int
	// SecretKeys declares credential fields for masked API responses and unchanged-value updates. Each adapter owns this knowledge; a whole webhook URL may itself be a credential.
	SecretKeys() []string
	// DestinationKeys declares fields controlling the recipient endpoint. Changing a destination while retaining stored credentials would let an editor exfiltrate them; see PrepareConfigUpdate.
	DestinationKeys() []string
}

// registry explicitly lists adapters rather than depending on init side effects, making supported channels and omissions visible in one place.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get resolves a channel adapter by kind.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether an adapter is registered.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns supported types in stable lexical order for UI selectors.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks failures such as bad credentials, rejected destinations or invalid payloads. Retry only transient network, rate-limit and server failures.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent wraps a nonnil error as permanent and preserves nil for convenient validation returns.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent checks the error chain for a permanent-failure marker.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// Configuration helpers normalize JSONB values: numbers decode as float64 and arrays as []any. They also tolerate simple UI type differences such as string-valued ports.

// cfgString reads and trims a string value, including whitespace introduced by pasted form data.
func cfgString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// cfgInt accepts JSON numeric and string representations.
func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// cfgBool also accepts string true or 1.
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// cfgStrings trims a string list and removes empty values.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// Accept a scalar string for a single-value form submission.
		if s := cfgString(cfg, key); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cfgMap reads a trimmed string map, such as custom HTTP headers, dropping empty keys.
func cfgMap(cfg map[string]any, key string) map[string]string {
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}

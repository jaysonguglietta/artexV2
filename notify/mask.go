package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix marks API credential placeholders. Updates receiving that prefix preserve the stored value. A recognizable suffix helps users identify the correct robot without pasting its secret again.
const MaskedPrefix = "__masked__"

// MaskedValue returns __masked__ for short values, or __masked__: followed by the final six characters. Webhook identifiers live near the end; the common URL prefix has little identifying value. Reveal only this limited hint.
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked recognizes an unchanged API credential placeholder.
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig copies the config and masks channel credential fields. Unknown channel types return an empty map rather than potentially disclosing unrecognized secrets; nonsecret fields are retained for editing.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Treat nested structures such as headers as one credential rather than maintaining a separate secret-key classification for every child field.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials reports a destination change without an explicit credential decision; do not silently reuse or discard credentials.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // Destination keys that changed.
	Missing []string // Credential keys lacking an explicit decision.
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "Destination (" + strings.Join(e.Changed, ", ") + ") changed. Explicitly resubmit credential fields (" +
		strings.Join(e.Missing, ", ") + "): provide new values or clear them if credentials are no longer required. " +
		"Existing credentials belong to the previous destination and must not be forwarded automatically."
}

// PrepareConfigUpdate prevents credential exfiltration by changing only a destination while omitted secret fields retain stored values. Whenever a destination changes, every credential must be replaced or explicitly cleared. Omission or a returned mask means reuse and is rejected. Automatically discarding optional credentials would silently disable authentication while reporting success, so require an explicit operator decision.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("Unregistered channel type: %q", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// Reject a mask nested inside a nonstring credential object: MergeConfig only interprets whole masked strings, so nested sentinels would be stored literally and silently break authentication. Validate before the unchanged-destination early return too.
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Identify real destination changes; a mask means unchanged.
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// Without a destination change, apply ordinary merge semantics.
		return MergeConfig(stored, incoming), nil
	}

	// A changed destination requires an explicit decision for every credential.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects placeholders embedded in object/array fields. Nested credentials must be provided as a complete value or preserved with one whole masked string.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("Field %s contains the masked placeholder %q. Omit the entire field to retain its value or submit a complete replacement; nested masked placeholders are not permitted",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares JSON representations to normalize numeric types. Normalize empty strings and missing keys first because MergeConfig deletes explicitly blank fields; otherwise an unchanged optional Telegram base_url would repeatedly appear changed and unnecessarily demand the bot token again.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue uses the same trimmed-empty-string semantics as MergeConfig to avoid disagreement between deletion and comparison.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig preserves masked and omitted values, deletes explicitly empty string fields, and overwrites other supplied values. Empty is an explicit clear operation; omission expresses keeping a partial-update field unchanged.
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // A mask means unchanged: retain the stored value.
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}

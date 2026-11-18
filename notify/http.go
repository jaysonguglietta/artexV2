package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets controls loopback/link-local delivery, denied by default to prevent management-port and cloud-metadata access. A compromised configuration session could otherwise disclose response snippets through last_error and delivery history. ARTEX_NOTIFY_ALLOW_LOCAL=1 explicitly permits legitimate local SMTP relays and synthetic httptest receivers; tests use exported AllowLocalTargetsEnv.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP rejects loopback, link-local/metadata, unspecified and multicast addresses. RFC1918 ranges remain allowed for legitimate private Mattermost/SMTP deployments; this is an explicit usability/security tradeoff.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Normalize IPv4-mapped IPv6 before checking so addresses such as ::ffff:127.0.0.1 cannot bypass the filter.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the transport Control hook enforcing the final resolved address at connection time. Configuration-only checks cannot cover DNS rebinding or later redirects, including same-host redirects.
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("Could not resolve destination: %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("Local/link-local destination %s is blocked (set %s=1 only if local delivery is intended)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport clones the default transport and adds only the dial guard, retaining pooling, HTTP/2, timeouts and proxy behavior.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is shared by channel deliveries and intentionally independent of the assessment GlobalProxy, whose unstable target-network tunnels must not impair notifications. Use a 15-second deadline. Reject cross-host redirects because tokens may be embedded in URLs; same-host canonicalization redirects remain allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("Too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("Cross-host redirects are prohibited (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit bounds anomalous response bodies; delivery history needs only a code and short error description.
const respBodyLimit = 8 << 10

// doJSON sends one request and returns a bounded body. A nil payload means an empty body; custom headers are forwarded as configured. Network errors and 5xx/408/429 are retryable, while other 4xx failures are permanent.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// Serialization failure is a local configuration/type error, not something retries can repair.
			return nil, Permanent(fmt.Errorf("Could not construct request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// An invalid URL is normally a permanent configuration error. Do not expose parser errors containing the full credential-bearing URL.
		return nil, Permanent(fmt.Errorf("Invalid request URL: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refusal, DNS failure and timeouts are usually transient. Redact transport errors before returning them: url.Error embeds the full URL, and webhook/bot tokens must not leak through stored last_error, history APIs, logs or test-send responses.
		return nil, fmt.Errorf("Request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("Could not read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// Only 429 rate limits and 408 timeouts among 4xx responses are retryable; other client errors require configuration or permission changes.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("Remote server throttled the request or timed out (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("Remote server error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("Remote server rejected request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet collapses response whitespace into a short single-line error suitable for the history UI.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget retains only scheme://host and an ellipsis. Tokens may occur in query strings or arbitrary path segments, so per-channel selective redaction risks omissions. Host is sufficient for DNS/connection/certificate diagnostics; masked suffixes identify the robot. Parse failures return a fixed placeholder, never raw input.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unresolvable address)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError unwraps url.Error.Err instead of calling its URL-bearing Error method. Structured removal is safer than trying to match every encoded URL variant afterward.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: unknown error", uerr.Op, host)
	}
	// Non-url.Error values, including redirect-policy errors, may contain URLs and also require redaction.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces HTTP(S) URLs in unstructured errors with their redacted form, splitting at whitespace and quotes.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

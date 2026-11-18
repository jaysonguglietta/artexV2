package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Repo fixes the trusted upstream release source. Making it ordinary editable configuration would grant code-execution authority to any configuration editor; self-update remains disabled by default.
const Repo = "Autumn-27/artex"

// latestURL selects the latest non-draft, non-prerelease GitHub release.
const latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// allowedHosts restricts every release download/redirect hop. TLS and host checks constrain origin; SHA256SUMS verifies archive integrity but is not independent signed provenance.
var allowedHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true, // Release asset object-storage host.
	"release-assets.githubusercontent.com": true,
	"raw.githubusercontent.com":            true,
}

// Release contains the GitHub fields needed for updating.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset describes one release attachment.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// NewClient builds a fresh TLS-verifying, host-restricted transport, using only an explicit proxy. Do not inherit unrelated transports with disabled TLS checks.
func NewClient(proxy string) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Minute, // Allow the full package download rather than a short whole-request deadline.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("Too many redirects")
			}
			return checkURL(req.URL)
		},
	}
}

// checkURL requires HTTPS and an approved hostname.
func checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("Non-HTTPS URL rejected: %s", u.Scheme+"://"+u.Host)
	}
	if !allowedHosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("Non-GitHub host rejected: %s", u.Hostname())
	}
	return nil
}

// FetchLatest loads the latest formal release.
func FetchLatest(ctx context.Context, c *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "artex-selfupdate")

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Could not reach GitHub (configure an egress proxy in system settings if required): %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		// Unauthenticated GitHub API requests share a 60/hour IP budget, easily exhausted behind shared egress.
		return nil, fmt.Errorf("GitHub API rate limit reached. Retry later or review API authentication")
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("Repository %s has no published release", Repo)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("Failed to parse release: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return nil, fmt.Errorf("Release has no tag")
	}
	return &rel, nil
}

// AssetName matches build.sh naming: artex-<version>-<os>-<arch>.zip without a leading v in the version.
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("artex-%s-%s-%s.zip", strings.TrimPrefix(tag, "v"), goos, goarch)
}

// FindAsset locates a release attachment by name.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Asset{}, false
}

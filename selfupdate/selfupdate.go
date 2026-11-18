// Package selfupdate provides read-only release information and legacy staging
// fixtures. Production in-process binary replacement is disabled; installation
// requires external verification of this fork's signed release artifacts.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart asks the supervisor to relaunch without crash backoff. Zero stops normally; other exits are crashes.
const ExitRestart = 75

// maxAttempts bounds unstable post-swap starts before rollback; surviving the settle delay clears the marker.
const maxAttempts = 3

// Paths anchors all update files beside the executable rather than an arbitrary service working directory.
type Paths struct {
	Dir     string // Executable directory.
	Current string // Current executable.
	New     string // Staged replacement executable.
	Sum     string // Staged executable SHA256 digest.
	Old     string // Pre-update backup executable.
	Marker  string // Update state marker.
}

// ResolvePaths derives update paths from the executable. Keep .exe on staged/backup Windows binaries so smoke and replacement execution work.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("Failed to locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // Windows .exe suffix, usually empty on Unix.
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker tracks installation progress for unstable-version rollback.
type marker struct {
	From     string `json:"from"`     // Previous version.
	To       string `json:"to"`       // Target version.
	Attempts int    `json:"attempts"` // Post-swap startup attempts.
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes staging after success, failure or cancellation so a leftover binary is not retried on restart.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions returns -1/0/1 for clean comparable versions. Reject dev/git-describe forms so release updates cannot overwrite development work or misreport it as current.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion accepts only optional-v three-component versions. Reject git-describe suffixes rather than treating them as the corresponding formal release.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker identifies container execution. In-process replacement would affect only the writable layer and disappear on image recreation; the supported read-only deployment rebuilds images instead.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}

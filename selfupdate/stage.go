package selfupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

// sumsAsset names the release workflow checksum manifest covering ZIP attachments.
const sumsAsset = "SHA256SUMS"

// maxBinarySize bounds extracted executables to resist malformed archive disk exhaustion.
const maxBinarySize = 512 << 20 // 512 MiB

// Phase values are emitted directly in SSE progress events.
type Phase string

const (
	PhaseIdle     Phase = "idle"
	PhaseDownload Phase = "downloading"
	PhaseVerify   Phase = "verifying"
	PhaseExtract  Phase = "extracting"
	PhaseStaged   Phase = "staged"
	PhaseFailed   Phase = "failed"
)

// Progress pushes frontend status; pct is 0-100 during download and -1 otherwise.
type Progress func(ph Phase, pct int, msg string)

// Stage downloads the current-platform ZIP, verifies it and stages artex.new. ZIP checksums match historical releases and preserve packaged skills for future synchronization. Completion lets the caller shut down cleanly and exit ExitRestart.
func Stage(ctx context.Context, c *http.Client, rel *Release, currentVersion string, prog Progress) error {
	if prog == nil {
		prog = func(Phase, int, string) {}
	}
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if err := checkWritable(p.Dir); err != nil {
		return err
	}

	name := AssetName(rel.TagName, runtime.GOOS, runtime.GOARCH)
	asset, ok := rel.FindAsset(name)
	if !ok {
		return fmt.Errorf("This release has no package for %s/%s (missing %s)", runtime.GOOS, runtime.GOARCH, name)
	}

	prog(PhaseDownload, 0, "Fetching checksum list...")
	sums, err := fetchSums(ctx, c, rel)
	if err != nil {
		return err
	}
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("%s has no entry for %s; refusing to install an unverified binary", sumsAsset, name)
	}

	// Place temporary files in the destination directory so final rename stays atomic on one filesystem rather than crossing a separately mounted /tmp.
	zipPath := p.New + ".zip.part"
	binPath := p.New + ".part"
	defer func() {
		_ = os.Remove(zipPath)
		_ = os.Remove(binPath)
	}()

	prog(PhaseDownload, 0, fmt.Sprintf("Downloading %s (%s)...", name, humanSize(asset.Size)))
	got, err := download(ctx, c, asset, zipPath, prog)
	if err != nil {
		return err
	}

	prog(PhaseVerify, -1, "Verifying SHA256...")
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s (download damaged or modified)", short(want), short(got))
	}

	prog(PhaseExtract, -1, "Extracting and smoke testing...")
	if err := extractBinary(zipPath, binPath); err != nil {
		return err
	}
	if err := smokeTest(binPath); err != nil {
		return fmt.Errorf("The new version cannot run on this system: %w", err)
	}

	// Store a separate staged-binary digest and recheck before replacement to detect corruption or modification before restart.
	binSum, err := fileSHA256(binPath)
	if err != nil {
		return fmt.Errorf("Failed to hash the new binary: %w", err)
	}
	if err := os.WriteFile(p.Sum, []byte(binSum), 0o644); err != nil {
		return fmt.Errorf("Failed to write checksum: %w", err)
	}
	if err := os.Rename(binPath, p.New); err != nil {
		_ = os.Remove(p.Sum)
		return fmt.Errorf("Failed to prepare the new version: %w", err)
	}

	if err := writeMarker(p.Marker, marker{
		From:     currentVersion,
		To:       strings.TrimPrefix(rel.TagName, "v"),
		StagedAt: time.Now().Unix(),
	}); err != nil {
		// A marker-write failure affects automatic rollback, not the already completed staging.
		prog(PhaseStaged, -1, "Warning: could not write the update marker; automatic rollback protection is unavailable for this update")
	}

	prog(PhaseStaged, 100, "New version prepared; restarting...")
	return nil
}

// fetchSums downloads the filename-to-hex-digest checksum map.
func fetchSums(ctx context.Context, c *http.Client, rel *Release) (map[string]string, error) {
	asset, ok := rel.FindAsset(sumsAsset)
	if !ok {
		return nil, fmt.Errorf("This release has no %s; integrity cannot be verified, so the update is refused", sumsAsset)
	}
	body, err := get(ctx, c, asset.URL)
	if err != nil {
		return nil, fmt.Errorf("Failed to download %s: %w", sumsAsset, err)
	}
	defer body.Close()

	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", sumsAsset, err)
	}
	out := parseSums(string(raw))
	if len(out) == 0 {
		return nil, fmt.Errorf("%s is empty or has an unrecognized format", sumsAsset)
	}
	return out, nil
}

// parseSums accepts only 64-digit hexadecimal digests in sha256sum-style entries; arbitrary two-word text must not become a bogus checksum.
func parseSums(raw string) map[string]string {
	out := map[string]string{}
	for line := range strings.Lines(raw) {
		// Accept double-space filename separators and the optional binary-mode asterisk prefix.
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || !isHexSHA256(fields[0]) {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == "" {
			continue
		}
		out[name] = strings.ToLower(fields[0])
	}
	return out
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// download writes to dst while hashing and reporting Content-Length progress.
func download(ctx context.Context, c *http.Client, a Asset, dst string, prog Progress) (string, error) {
	body, err := get(ctx, c, a.URL)
	if err != nil {
		return "", fmt.Errorf("Failed to download %s: %w", a.Name, err)
	}
	defer body.Close()

	f, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("Failed to create temporary file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	pw := &progressWriter{total: a.Size, prog: prog, name: a.Name, last: time.Now()}
	if _, err := io.Copy(io.MultiWriter(f, h, pw), body); err != nil {
		return "", fmt.Errorf("Download interrupted: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("Disk write failed: %w", err)
	}
	if a.Size > 0 && pw.written != a.Size {
		return "", fmt.Errorf("Incomplete download: expected %d bytes, got %d bytes", a.Size, pw.written)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// get performs a host-restricted GET and returns its body.
func get(ctx context.Context, c *http.Client, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "artex-selfupdate")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// extractBinary finds the expected executable by basename inside the versioned platform archive, avoiding brittle full-directory reconstruction.
func extractBinary(zipPath, dst string) error {
	want := "artex"
	if runtime.GOOS == "windows" {
		want = "artex.exe"
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("Failed to open release archive: %w", err)
	}
	defer zr.Close()

	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() || !strings.EqualFold(path.Base(entry.Name), want) {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("Failed to read %s: %w", entry.Name, err)
		}
		defer rc.Close()

		f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return fmt.Errorf("Failed to write new binary: %w", err)
		}
		defer f.Close()

		n, err := io.Copy(f, io.LimitReader(rc, maxBinarySize+1))
		if err != nil {
			return fmt.Errorf("Failed to extract %s: %w", entry.Name, err)
		}
		if n > maxBinarySize {
			return fmt.Errorf("Executable in release archive exceeds %s; extraction refused", humanSize(maxBinarySize))
		}
		if n == 0 {
			return fmt.Errorf("The %s file in the release archive is empty", want)
		}
		return f.Sync()
	}
	return fmt.Errorf("The release archive contains no %s file", want)
}

// checkWritable detects permission problems before downloading a large package that cannot later be staged.
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".artex-update-probe-*")
	if err != nil {
		return fmt.Errorf("Cannot write program directory %s; automatic update unavailable (check permissions or update manually): %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// progressWriter tracks bytes and throttles SSE updates rather than emitting one per 32 KiB chunk.
type progressWriter struct {
	total   int64
	written int64
	name    string
	prog    Progress
	last    time.Time
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.written += int64(len(b))
	if time.Since(w.last) < 300*time.Millisecond {
		return len(b), nil
	}
	w.last = time.Now()
	pct := -1
	if w.total > 0 {
		pct = int(w.written * 100 / w.total)
	}
	w.prog(PhaseDownload, pct, fmt.Sprintf("Downloading %s / %s", humanSize(w.written), humanSize(w.total)))
	return len(b), nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12] + "…"
	}
	return sum
}

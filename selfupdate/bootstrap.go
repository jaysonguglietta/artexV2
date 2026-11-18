package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// smokeEnv explicitly skips Bootstrap in smoke-test subprocesses. Although the staged executable normally derives separate .new paths, relying on naming coincidence is fragile and adds unnecessary filesystem probing.
const smokeEnv = "ARTEX_SELFUPDATE_SMOKE"

// Action tells main how to proceed after Bootstrap.
type Action int

const (
	// Continue starts the server normally.
	Continue Action = iota
	// Restart exits with ExitRestart so the supervisor relaunches immediately.
	Restart
)

// State reports startup update/rollback outcomes accurately to the frontend.
type State struct {
	Pending     bool   // The swapped version has not yet demonstrated stable runtime.
	RolledBack  bool   // This startup performed an automatic rollback.
	FailedStage bool   // Staged verification/smoke failed and the staged binary was discarded.
	Detail      string // User-facing summary.
}

// Bootstrap runs before database opening or network listeners. Verify/smoke/swap a staged binary and request restart; otherwise count unsettled post-swap attempts and roll back when exhausted; without staging/marker state, start normally.
func Bootstrap() (Action, State) {
	if os.Getenv(smokeEnv) != "" {
		return Continue, State{}
	}
	p, err := ResolvePaths()
	if err != nil {
		log.Printf("[update] skipping bootstrap: %v", err)
		return Continue, State{}
	}

	if _, err := os.Stat(p.New); err == nil {
		return applyStaged(p)
	}

	m, ok := readMarker(p.Marker)
	if !ok {
		return Continue, State{}
	}
	return confirmOrRollback(p, m)
}

// applyStaged is the final executable-replacement gate. Verification and a real execution smoke reject corrupt downloads, wrong architectures and missing dependencies before a supervisor can enter an unrecoverable exec-failure loop.
func applyStaged(p Paths) (Action, State) {
	m, _ := readMarker(p.Marker)

	if err := verifyStaged(p); err != nil {
		log.Printf("[update] staged version failed verification, discarded; continuing on current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "New version failed verification and was discarded: " + err.Error()}
	}

	if err := swap(p); err != nil {
		log.Printf("[update] version swap failed, continuing on current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "Version replacement failed: " + err.Error()}
	}

	// Swap succeeded; leave the marker for the new version to prove stability on its next startup.
	m.Attempts = 0
	if m.StagedAt == 0 {
		m.StagedAt = time.Now().Unix()
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to write upgrade marker (automatic rollback unavailable): %v", err)
	}
	log.Printf("[update] swapped to %s, exiting to restart (exit %d)", orUnknown(m.To), ExitRestart)
	return Restart, State{Pending: true}
}

// confirmOrRollback counts executions that reach Go initialization, covering startup/config/migration failures. The pre-swap smoke separately catches binaries that cannot execute at all.
func confirmOrRollback(p Paths, m marker) (Action, State) {
	m.Attempts++
	if m.Attempts > maxAttempts {
		if err := rollback(p); err != nil {
			// If rollback itself fails, clear the marker and stop restart cycling so logs can expose the remaining startup problem.
			log.Printf("[update] new version failed to start %d times in a row and rollback failed: %v", maxAttempts, err)
			_ = os.Remove(p.Marker)
			return Continue, State{Detail: "New version failed to start and rollback failed: " + err.Error()}
		}
		log.Printf("[update] new version failed to start %d times in a row, rolled back to %s, exiting to restart (exit %d)",
			maxAttempts, orUnknown(m.From), ExitRestart)
		_ = os.Remove(p.Marker)
		return Restart, State{RolledBack: true, Detail: fmt.Sprintf("New version failed to start; rolled back to previous version (%s)", orUnknown(m.From))}
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to update upgrade marker: %v", err)
	}
	log.Printf("[update] starting new version (attempt %d/%d), upgrade is confirmed once it runs stably",
		m.Attempts, maxAttempts)
	return Continue, State{Pending: true}
}

// Settle clears the upgrade marker only after a running HTTP server survives the confirmation delay. Earlier failures retain the marker for attempt counting and eventual rollback.
func Settle() {
	p, err := ResolvePaths()
	if err != nil {
		return
	}
	settle(p)
}

func settle(p Paths) {
	if _, ok := readMarker(p.Marker); !ok {
		return // Not a post-update startup; nothing to settle.
	}
	if err := os.Remove(p.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[update] failed to clear upgrade marker: %v", err)
		return
	}
	log.Printf("[update] new version is stable, upgrade complete (previous version kept as %s)", p.Old)
}

// SettleDelay is the required stable runtime after update.
const SettleDelay = 30 * time.Second

// verifyStaged checks SHA256 and executes a smoke test.
func verifyStaged(p Paths) error {
	want, err := os.ReadFile(p.Sum)
	if err != nil {
		return fmt.Errorf("Failed to read checksum: %w", err)
	}
	got, err := fileSHA256(p.New)
	if err != nil {
		return fmt.Errorf("Failed to compute checksum: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(want)), got) {
		return errors.New("SHA256 mismatch (download damaged or modified)")
	}
	return smokeTest(p.New)
}

// smokeTest runs the new binary with -h to reject truncation, wrong architecture and missing runtime dependencies.
func smokeTest(bin string) error {
	if err := os.Chmod(bin, 0o755); err != nil {
		return fmt.Errorf("Failed to grant executable permission: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-h")
	cmd.Env = append(os.Environ(), smokeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return errors.New("Smoke test timed out (new binary did not respond)")
	}
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("Smoke test failed: %v: %s", err, snippet)
	}
	return nil
}

// swap renames the current executable to its backup then installs staging. Unix/Windows allow renaming a running executable; Windows restrictions concern deleting/overwriting it.
func swap(p Paths) error {
	// Remove the previous backup first because Windows rename cannot overwrite an existing destination.
	if err := os.Remove(p.Old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Failed to remove previous backup %s: %w", p.Old, err)
	}
	if err := os.Rename(p.Current, p.Old); err != nil {
		return fmt.Errorf("Failed to back up current version: %w", err)
	}
	if err := os.Rename(p.New, p.Current); err != nil {
		// If staging installation fails after moving the current binary, restore the original so the next startup still has an executable.
		if rerr := os.Rename(p.Old, p.Current); rerr != nil {
			return fmt.Errorf("New version installation failed (%v), and current-version recovery failed: %w", err, rerr)
		}
		return fmt.Errorf("Failed to install new version: %w", err)
	}
	_ = os.Remove(p.Sum)
	return nil
}

// rollback restores the version backed up by swap.
func rollback(p Paths) error {
	if _, err := os.Stat(p.Old); err != nil {
		return fmt.Errorf("No rollback backup (%s): %w", p.Old, err)
	}
	// Retain the failed new binary for diagnosis rather than deleting it.
	failed := p.Current + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(p.Current, failed); err != nil {
		return fmt.Errorf("Failed to move failed version: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		return fmt.Errorf("Failed to restore previous version: %w", err)
	}
	return nil
}

// Rollback handles the explicitly enabled rollback endpoint by swapping versions; the caller exits with ExitRestart for supervisor relaunch.
func Rollback() error {
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Old); err != nil {
		return errors.New("No previous version for rollback (" + p.Old + " missing)")
	}
	cleanStaged(p)
	if err := smokeTest(p.Old); err != nil {
		return fmt.Errorf("Previous version is not executable; rollback refused: %w", err)
	}
	// Exchange current and backup so a later rollback can reverse the choice.
	tmp := p.Current + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(p.Current, tmp); err != nil {
		return fmt.Errorf("Failed to move current version: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		_ = os.Rename(tmp, p.Current)
		return fmt.Errorf("Failed to install previous version: %w", err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		log.Printf("[update] failed to tidy backups after rollback (no runtime impact): %v", err)
	}
	_ = os.Remove(p.Marker)
	return nil
}

// HasBackup controls whether the UI can offer a prior-version rollback.
func HasBackup() bool {
	p, err := ResolvePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.Old)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Unknown version"
	}
	return s
}

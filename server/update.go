package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Autumn-27/artex/selfupdate"
)

// HTTP surface for deliberately enabled updates. selfupdate owns downloading, verification and replacement; this layer owns authentication, exclusion, progress and the restart signal. Staging ends with ExitRestart and start.sh/start.bat supervises relaunch.

// Update progress, reasons and errors are displayed verbatim by the frontend update card; keep them English.
const (
	updateMsgPreparing          = "Preparing…"
	updateMsgFailed             = "Update failed"
	updateMsgStaged             = "New version staged. Restarting..."
	updateErrNotReleaseFmt      = "Current version %q is not a release; in-process updates are unavailable"
	updateErrAlreadyLatestFmt   = "Already up to date (%s)"
	updateErrInProgress         = "An update is already running"
	updateErrRollbackInProgress = "Cannot roll back while an update is running"
)

// restartCh closes once an upgrade or rollback is ready; main then exits with ExitRestart.
var (
	restartOnce sync.Once
	restartCh   = make(chan struct{})
)

// RestartRequested closes when the supervisor should relaunch this process.
func RestartRequested() <-chan struct{} { return restartCh }

func requestRestart() { restartOnce.Do(func() { close(restartCh) }) }

// bootState records the startup Bootstrap outcome: success, rollback or discarded staging. The check endpoint reports the actual previous upgrade outcome.
var (
	bootStateMu sync.Mutex
	bootState   selfupdate.State
)

// SetBootUpdateState is called once by main during startup.
func SetBootUpdateState(st selfupdate.State) {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	bootState = st
}

func bootUpdateState() selfupdate.State {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	return bootState
}

// releaseCache limits repeated GitHub latest-release requests from page loads and multiple tabs. Unauthenticated API rate limits make caching necessary; an explicit user check can force a refresh.
type releaseCache struct {
	mu  sync.Mutex
	rel *selfupdate.Release
	err error
	at  time.Time
	// fetch is an injected test function; nil uses the real GitHub query.
	fetch func(context.Context, *http.Client) (*selfupdate.Release, error)
}

const (
	releaseTTL = 30 * time.Minute
	// Cache failures briefly to avoid repeated timeout delays while GitHub is unreachable, but recover soon after connectivity returns.
	releaseErrTTL = 2 * time.Minute
	// Bound version checks separately from the client's 30-minute whole-release download timeout.
	releaseTimeout = 20 * time.Second
)

var relCache = &releaseCache{}

// get returns the cached or freshly queried latest release. Hold the lock through fetching so concurrent tabs share one upstream request.
func (c *releaseCache) get(ctx context.Context, client *http.Client, force bool) (*selfupdate.Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force {
		ttl := releaseTTL
		if c.err != nil {
			ttl = releaseErrTTL
		}
		if !c.at.IsZero() && time.Since(c.at) < ttl {
			return c.rel, c.err
		}
	}

	fetch := c.fetch
	if fetch == nil {
		fetch = selfupdate.FetchLatest
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	rel, err := fetch(ctx, client)
	// A closed tab canceling its request is not a GitHub failure; do not cache cancellation for the next caller.
	if err != nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
		return c.rel, err
	}
	c.rel, c.err, c.at = rel, err, time.Now()
	return rel, err
}

// updateProgress is one frontend progress event.
type updateProgress struct {
	Phase   selfupdate.Phase `json:"phase"`
	Percent int              `json:"percent"` // Meaningful only during downloading; otherwise -1.
	Message string           `json:"message"`
	Version string           `json:"version,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// updateHub stores and broadcasts one update's progress. running also excludes concurrent apply requests with HTTP 409 so two writers cannot target artex.new.
type updateHub struct {
	mu      sync.Mutex
	running bool
	cur     updateProgress
	subs    map[chan updateProgress]struct{}
}

var updHub = &updateHub{
	cur:  updateProgress{Phase: selfupdate.PhaseIdle, Percent: -1},
	subs: map[chan updateProgress]struct{}{},
}

// begin claims update ownership and returns false if one is already running.
func (h *updateHub) begin(version string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.running = true
	h.cur = updateProgress{Phase: selfupdate.PhaseDownload, Percent: 0, Message: updateMsgPreparing, Version: version}
	h.fanout(h.cur)
	return true
}

// finish ends an attempt; nil error means staging succeeded and restart is pending.
func (h *updateHub) finish(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	if err != nil {
		h.cur = updateProgress{Phase: selfupdate.PhaseFailed, Percent: -1, Message: updateMsgFailed, Error: err.Error(), Version: h.cur.Version}
	} else {
		h.cur = updateProgress{Phase: selfupdate.PhaseStaged, Percent: 100, Message: updateMsgStaged, Version: h.cur.Version}
	}
	h.fanout(h.cur)
}

func (h *updateHub) publish(ph selfupdate.Phase, pct int, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cur = updateProgress{Phase: ph, Percent: pct, Message: msg, Version: h.cur.Version}
	h.fanout(h.cur)
}

// fanout requires h.mu. Subscriber channels are buffered and may drop progress; a stalled SSE consumer must never block the upgrade.
func (h *updateHub) fanout(p updateProgress) {
	for ch := range h.subs {
		select {
		case ch <- p:
		default:
		}
	}
}

func (h *updateHub) snapshot() (updateProgress, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur, h.running
}

func (h *updateHub) subscribe() (<-chan updateProgress, func()) {
	ch := make(chan updateProgress, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// updateCheck compares the latest official release with the current version. Backend reachability is authoritative because the backend downloads artifacts; browser-only GitHub connectivity cannot make an update succeed.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion
	mode := "binary"
	if selfupdate.InDocker() {
		mode = "docker"
	}
	boot := bootUpdateState()
	out := map[string]any{
		"current":     current,
		"mode":        mode,
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"has_backup":  selfupdate.HasBackup(),
		"repo":        selfupdate.Repo,
		"boot_notice": boot.Detail,
		"rolled_back": boot.RolledBack,
	}

	// The top bar uses cache by default; explicit user checks send force=1.
	force := r.URL.Query().Get("force") != ""
	client := selfupdate.NewClient(s.m.GlobalProxy())
	rel, err := relCache.get(r.Context(), client, force)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, 200, out)
		return
	}

	latest := rel.TagName
	out["latest"] = latest
	out["notes"] = rel.Body
	out["html_url"] = rel.HTMLURL
	if !rel.PublishedAt.IsZero() {
		out["published_at"] = rel.PublishedAt.Format(time.RFC3339)
	}

	asset := selfupdate.AssetName(latest, runtime.GOOS, runtime.GOARCH)
	out["asset"] = asset
	if a, ok := rel.FindAsset(asset); ok {
		out["asset_available"] = true
		out["size"] = a.Size
	} else {
		out["asset_available"] = false
	}

	cmp, comparable := selfupdate.CompareVersions(current, latest)
	out["comparable"] = comparable
	out["has_update"] = comparable && cmp < 0
	if !comparable {
		// Development/git-describe builds have no comparable release version. Refuse to overwrite a locally debugged binary with an official release.
		out["reason"] = fmt.Sprintf(updateErrNotReleaseFmt, current)
	}
	writeJSON(w, 200, out)
}

// updateApply starts a background download/staging operation and immediately returns 202; long downloads cannot depend on a reverse-proxy request deadline. Progress streams separately.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ARTEX_ENABLE_SELF_UPDATE") != "1" {
		writeErr(w, 403, "In-process updates are disabled. Deploy a reviewed, pinned build instead.")
		return
	}

	current := BuildVersion

	// Use the cached release the user actually reviewed and confirmed.
	client := selfupdate.NewClient(s.m.GlobalProxy())
	rel, err := relCache.get(r.Context(), client, false)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	cmp, comparable := selfupdate.CompareVersions(current, rel.TagName)
	if !comparable {
		writeErr(w, 400, fmt.Sprintf(updateErrNotReleaseFmt, current))
		return
	}
	if cmp >= 0 {
		writeErr(w, 400, fmt.Sprintf(updateErrAlreadyLatestFmt, current))
		return
	}
	if !updHub.begin(rel.TagName) {
		writeErr(w, 409, updateErrInProgress)
		return
	}

	go func() {
		// Use s.ctx rather than the request context, which ends as soon as the HTTP response is returned.
		err := selfupdate.Stage(s.ctx, client, rel, current, func(ph selfupdate.Phase, pct int, msg string) {
			updHub.publish(ph, pct, msg)
		})
		updHub.finish(err)
		if err != nil {
			log.Printf("[update] update failed: %v", err)
			return
		}
		log.Printf("[update] %s -> %s staged, exiting to complete the swap", current, rel.TagName)
		// Allow the final progress event to reach the frontend before exiting.
		time.Sleep(1500 * time.Millisecond)
		requestRestart()
	}()

	writeJSON(w, 202, map[string]any{"ok": true, "target": rel.TagName})
}

// updateRollback restores the pre-replacement artex.old backup.
func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ARTEX_ENABLE_SELF_UPDATE") != "1" {
		writeErr(w, 403, "In-process updates are disabled. Deploy a reviewed, pinned build instead.")
		return
	}

	if _, running := updHub.snapshot(); running {
		writeErr(w, 409, updateErrRollbackInProgress)
		return
	}
	if err := selfupdate.Rollback(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	log.Printf("[update] manually rolled back to the previous version, exiting to complete the switch")
	writeJSON(w, 202, map[string]any{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		requestRestart()
	}()
}

// updateStream emits update progress through SSE.
func (s *Server) updateStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := updHub.subscribe()
	defer unsub()

	send := func(p updateProgress) {
		b, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	// Send the current state first so refreshed pages immediately catch up.
	cur, _ := updHub.snapshot()
	send(cur)

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			send(p)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

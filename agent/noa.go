package agent

import (
	"log"
	"path/filepath"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/noaadapter"
)

// noaWarn returns a diagnostics sink tagging non-fatal noa messages with the
// session, routed through the package logger (agents have no per-instance one).
func noaWarn(session string) func(string) {
	return func(msg string) { log.Printf("[noa] %s: %s", session, msg) }
}

// noa is Norma's experimental model-driven context compression, selected in system settings. It is mutually exclusive with built-in compaction. noaadapter.Enable installs the compactor, Compress tool and code-owned prompt sections; without it, built-in compaction remains active. Read the resolver once per run, affecting subsequent runs without rebuilding agents.

// enableNoa installs the feature only when its resolver enables it. Archive originals under the global workDir/noa with globally unique session directories. Initialization failure warns and falls back to built-in compaction rather than interrupting a real task. On success clear Compaction to avoid competing managers.
func enableNoa(opts *agentcore.Options, enabled func() bool, archiveRoot, sessionID string, onWarn func(string)) {
	if enabled == nil || !enabled() {
		return
	}
	if opts.OnWarn == nil {
		opts.OnWarn = onWarn
	}
	if err := noaadapter.Enable(opts, noaadapter.Options{
		ArchiveBaseDir: filepath.Join(archiveRoot, "noa"),
		SessionID:      sessionID,
		OnWarn:         onWarn,
	}); err != nil {
		if onWarn != nil {
			onWarn("Could not enable noa compaction; falling back to built-in compaction:" + err.Error())
		}
		return
	}
	// Compactor wins over Compaction, but retaining both produces SDK warnings; clear the unused configuration explicitly.
	opts.Compaction = nil
}

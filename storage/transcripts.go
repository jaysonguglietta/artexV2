package storage

import (
	"errors"
	"os"
	"path/filepath"
)

const MaxRawTranscriptBytes int64 = 256 << 20
const MaxRawTranscriptInodes int64 = 8192

func ValidateRawTranscripts(dataDir string) error {
	mode := os.Getenv("ARTEX_RAW_TRANSCRIPTS")
	if mode == "" || mode == "off" {
		return nil
	}
	if mode != "ephemeral" {
		return errors.New("ARTEX_RAW_TRANSCRIPTS must be off or ephemeral")
	}
	return validateTranscriptFilesystem(filepath.Join(dataDir, "transcripts"))
}
func RawTranscriptsEnabled() bool { return os.Getenv("ARTEX_RAW_TRANSCRIPTS") == "ephemeral" }

type TranscriptStatus struct {
	Enabled         bool   `json:"enabled"`
	Ephemeral       bool   `json:"ephemeral"`
	LimitBytes      int64  `json:"limit_bytes"`
	LimitInodes     int64  `json:"limit_inodes"`
	AvailableBytes  int64  `json:"available_bytes"`
	AvailableInodes int64  `json:"available_inodes"`
	Error           string `json:"error,omitempty"`
}

func RawTranscriptStatus(dataDir string) TranscriptStatus {
	out := TranscriptStatus{Enabled: RawTranscriptsEnabled(), Ephemeral: true, LimitBytes: MaxRawTranscriptBytes, LimitInodes: MaxRawTranscriptInodes}
	if !out.Enabled {
		return out
	}
	bytes, inodes, err := transcriptFilesystemSpace(filepath.Join(dataDir, "transcripts"))
	out.AvailableBytes = bytes
	out.AvailableInodes = inodes
	if err != nil {
		out.Error = err.Error()
	} else if bytes < 1<<20 || inodes < 16 {
		out.Error = "raw transcript capacity is exhausted or nearly exhausted; SDK trace writes may be skipped, while PostgreSQL-backed history remains available"
	}
	return out
}

//go:build !linux

package storage

import "errors"

func validateTranscriptFilesystem(path string) error {
	return errors.New("ephemeral raw transcripts require a verified bounded Linux tmpfs")
}
func transcriptFilesystemSpace(path string) (int64, int64, error) {
	return 0, 0, validateTranscriptFilesystem(path)
}

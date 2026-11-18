//go:build linux

package storage

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func transcriptFS(path string) (unix.Statfs_t, error) {
	var info unix.Statfs_t
	entry, err := os.Lstat(path)
	if err != nil {
		return info, err
	}
	if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
		return info, errors.New("raw transcripts require an ordinary dedicated tmpfs directory")
	}
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	err = unix.Fstatfs(int(f.Fd()), &info)
	return info, err
}
func validateTranscriptFilesystem(path string) error {
	info, err := transcriptFS(path)
	if err != nil {
		return err
	}
	if info.Type != unix.TMPFS_MAGIC || info.Bsize <= 0 || info.Blocks == 0 || info.Blocks > uint64(MaxRawTranscriptBytes)/uint64(info.Bsize) || info.Files == 0 || info.Files > uint64(MaxRawTranscriptInodes) {
		return errors.New("ephemeral raw transcripts require Linux tmpfs with size at most 256 MiB and nr_inodes at most 8192")
	}
	return nil
}
func transcriptFilesystemSpace(path string) (int64, int64, error) {
	if err := validateTranscriptFilesystem(path); err != nil {
		return 0, 0, err
	}
	info, err := transcriptFS(path)
	if err != nil {
		return 0, 0, err
	}
	return int64(info.Bavail) * info.Bsize, int64(info.Ffree), nil
}

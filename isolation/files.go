package isolation

import (
	"bytes"
	"errors"
	"github.com/Autumn-27/artex/storage"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

const (
	maxArtifactFiles       = 1000
	maxArtifactPathBytes   = 1024
	maxArtifactPathDepth   = 32
	maxArtifactDirectories = 2048
)

func validateArtifactPath(name string) error {
	if err := storage.ValidatePath(name); err != nil {
		return err
	}
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, "\\\x00") {
		return errors.New("invalid artifact path")
	}
	if len(name) > maxArtifactPathBytes || strings.Count(name, "/")+1 > maxArtifactPathDepth {
		return errors.New("artifact path length or depth exceeds limit")
	}
	for component := range strings.SplitSeq(name, "/") {
		if len(component) > 255 {
			return errors.New("artifact path component exceeds limit")
		}
	}
	return nil
}

// Validate the complete untrusted map before creating directories or changing
// files. Content-byte limits alone do not constrain empty files or inode use.
func validateArtifactFiles(files map[string][]byte, limit int64) error {
	if limit < 0 || len(files) > maxArtifactFiles {
		return errors.New("artifact write limit exceeded")
	}
	parents := make(map[string]struct{})
	var used int64
	for name, data := range files {
		if err := validateArtifactPath(name); err != nil {
			return err
		}
		if int64(len(data)) > limit-used {
			return errors.New("artifact write limit exceeded")
		}
		used += int64(len(data))
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			parents[parent] = struct{}{}
			if len(parents) > maxArtifactDirectories {
				return errors.New("artifact directory count exceeds limit")
			}
			if _, fileExists := files[parent]; fileExists {
				return errors.New("artifact path conflicts with a parent file")
			}
		}
	}
	return nil
}

// Only regular files are synchronized. os.Root rejects symlink escapes on both
// sides; path, directory and byte limits bound sparse files and inode exhaustion.
func ReadFiles(dir string, limit int64) (map[string][]byte, error) {
	if limit < 0 {
		return nil, errors.New("invalid artifact read limit")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := map[string][]byte{}
	var used int64
	directories := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name != "." {
			if err := validateArtifactPath(name); err != nil {
				return err
			}
		}
		if d.IsDir() {
			if name != "." {
				directories++
				if directories > maxArtifactDirectories {
					return errors.New("artifact directory count exceeds limit")
				}
			}
			return nil
		}
		if len(files) >= maxArtifactFiles {
			return errors.New("artifact file count exceeds limit")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > limit-used {
			return errors.New("artifact synchronization exceeds limit; use worker Read for large files")
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, limit-used+1))
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		used += int64(len(data))
		if used > limit {
			return errors.New("artifact synchronization exceeds limit")
		}
		files[name] = data
		return nil
	})
	return files, err
}
func writeFilesRaw(dir string, files map[string][]byte) error {
	if err := validateArtifactFiles(files, 8<<20); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for name, data := range files {
		if err = root.MkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// ApplySnapshot preserves UI files created or changed while a tool was running.
// A worker may remove only the unchanged inputs it was actually given.
func applySnapshotRaw(dir string, sent, returned map[string][]byte) error {
	// Reject malicious worker paths and inode amplification before deleting any
	// unchanged API files from the prior snapshot.
	if err := validateArtifactFiles(returned, 8<<20); err != nil {
		return err
	}
	current, err := ReadFiles(dir, 8<<20)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for name, original := range sent {
		if _, exists := returned[name]; !exists && bytes.Equal(current[name], original) {
			if err = root.Remove(name); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	writable := map[string][]byte{}
	for name, data := range returned {
		actual, exists := current[name]
		original, wasSent := sent[name]
		if exists && (!wasSent || !bytes.Equal(actual, original)) {
			continue
		}
		writable[name] = data
	}
	return writeFilesRaw(dir, writable)
}

func artifactReservation(files map[string][]byte) storage.Reservation {
	var out storage.Reservation
	parents := map[string]bool{}
	for name, data := range files {
		out.Bytes += int64(len(data))
		out.Entries++
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			parents[parent] = true
		}
	}
	out.Entries += int64(len(parents))
	return out
}
func WriteFiles(dir string, files map[string][]byte) error {
	if err := validateArtifactFiles(files, 8<<20); err != nil {
		return err
	}
	return storage.GuardUnheld(dir, artifactReservation(files), func() error { return writeFilesRaw(dir, files) })
}
func ApplySnapshot(dir string, sent, returned map[string][]byte) error {
	if err := validateArtifactFiles(returned, 8<<20); err != nil {
		return err
	}
	return storage.GuardUnheld(dir, artifactReservation(returned), func() error { return applySnapshotRaw(dir, sent, returned) })
}

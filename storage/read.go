package storage

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// OpenRegular never follows a final symlink, confines intermediate components to
// the supplied root, and avoids blocking on a raced-in FIFO before f.Stat.
func OpenRegular(directory, name string) (*os.File, error) {
	if name == "." || !filepath.IsLocal(name) {
		return nil, errors.New("invalid rooted file path")
	}
	rootInfo, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("file root must be an ordinary directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("stored body must be an ordinary file")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, errors.New("stored body changed while opening")
	}
	return file, nil
}

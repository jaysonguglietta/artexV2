package server

import (
	"crypto/sha256"
	"errors"
	"github.com/Autumn-27/artex/storage"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Raw transcript tmpfs is a different filesystem. Copies remain private in the
// archive stage until commit; rollback never depends on a cross-device rename.
func copyArchivePath(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("archive copies cannot follow symlinks")
	}
	if info.Mode().IsRegular() {
		return copyArchiveFile(source, destination, info.Size())
	}
	if !info.IsDir() {
		return errors.New("archive copies require ordinary files/directories")
	}
	if err = storage.MkdirAll(destination, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return storage.MkdirAll(filepath.Join(destination, name), 0700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("archive copies cannot follow symlinks or special files")
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		return copyArchiveReader(file, filepath.Join(destination, name), info.Size())
	})
}
func copyArchiveFile(source, destination string, length int64) error {
	root, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(source))
	if err != nil {
		return err
	}
	defer file.Close()
	return copyArchiveReader(file, destination, length)
}
func copyArchiveReader(source io.ReadSeeker, destination string, length int64) error {
	err := storage.AtomicWrite(destination, source, length, true)
	if !os.IsExist(err) {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(destination))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != length {
		return errors.New("archive copy destination conflicts with retained data")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	a, b := sha256.New(), sha256.New()
	if _, err = io.Copy(a, source); err != nil {
		return err
	}
	if _, err = io.Copy(b, file); err != nil {
		return err
	}
	if string(a.Sum(nil)) != string(b.Sum(nil)) {
		return errors.New("archive copy changed; both versions were retained for operator review")
	}
	return nil
}

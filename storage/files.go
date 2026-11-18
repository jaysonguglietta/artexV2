package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BudgetFile admits every write chunk before changing a file. It is intended for
// newly created archive/export streams; callers remove partial files on error.
type BudgetFile struct {
	*os.File
	bucket *Bucket
}

func (f *BudgetFile) Write(data []byte) (n int, err error) {
	if f.bucket == nil {
		return f.File.Write(data)
	}
	err = f.bucket.WithReservation(Reservation{Bytes: int64(len(data))}, func() error { var writeErr error; n, writeErr = f.File.Write(data); return writeErr })
	return
}
func CreateFile(name string, flags int, mode os.FileMode) (out *BudgetFile, err error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	b := ForPath(name)
	if b == nil {
		f, err := os.OpenFile(name, flags, mode)
		if err != nil {
			return nil, err
		}
		return &BudgetFile{File: f}, nil
	}
	relative, err := filepath.Rel(b.root, name)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, errors.New("invalid managed storage path")
	}
	reserve := Reservation{Entries: int64(strings.Count(filepath.ToSlash(relative), "/")) + 1}
	err = b.WithReservation(reserve, func() error {
		root, err := os.OpenRoot(b.root)
		if err != nil {
			return err
		}
		defer root.Close()
		if err = root.MkdirAll(filepath.Dir(relative), 0700); err != nil {
			return err
		}
		f, err := root.OpenFile(relative, flags, mode)
		if err != nil {
			return err
		}
		out = &BudgetFile{File: f, bucket: b}
		return nil
	})
	return
}

// AtomicWrite reserves the full additional copy before opening a temporary file.
// A short/oversized reader or quota error leaves an existing destination intact.
func AtomicWrite(name string, source io.Reader, length int64, exclusive bool) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	if length < 0 {
		return errors.New("negative expected file length")
	}
	b := ForPath(name)
	rootDir := filepath.Dir(name)
	relative := filepath.Base(name)
	if b != nil {
		rootDir = b.root
		var err error
		relative, err = filepath.Rel(rootDir, name)
		if err != nil || !filepath.IsLocal(relative) {
			return errors.New("invalid managed storage path")
		}
	}
	if exclusive {
		check := func() error {
			root, err := os.OpenRoot(rootDir)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			defer root.Close()
			if _, err = root.Lstat(relative); err == nil {
				return os.ErrExist
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		var err error
		if b == nil {
			err = check()
		} else {
			err = b.WithReservation(Reservation{}, check)
		}
		if err != nil {
			return err
		}
	}
	work := func() error {
		if b == nil {
			if err := os.MkdirAll(rootDir, 0700); err != nil {
				return err
			}
		}
		root, err := os.OpenRoot(rootDir)
		if err != nil {
			return err
		}
		defer root.Close()
		if err = root.MkdirAll(filepath.Dir(relative), 0700); err != nil {
			return err
		}
		if info, err := root.Lstat(relative); err == nil {
			if exclusive {
				return os.ErrExist
			}
			if !info.Mode().IsRegular() {
				return errors.New("destination must be an ordinary file")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		var f *os.File
		var temporary string
		for attempt := 0; attempt < 10; attempt++ {
			temporary = filepath.Join(filepath.Dir(relative), ".writing-"+randomID())
			f, err = root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if !os.IsExist(err) {
				break
			}
		}
		if err != nil {
			return err
		}
		defer func() { f.Close(); _ = root.Remove(temporary) }()
		if _, err = io.CopyN(f, source, length); err != nil {
			return err
		}
		var extra [1]byte
		if n, readErr := io.ReadFull(source, extra[:]); n != 0 || readErr != io.EOF {
			return errors.New("input exceeds declared file length")
		}
		if err = f.Sync(); err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		return root.Rename(temporary, relative)
	}
	if b == nil {
		return work()
	}
	reserve := Reservation{Bytes: length, Entries: int64(strings.Count(filepath.ToSlash(relative), "/")) + 2}
	if !exclusive {
		return b.WithReservationUnheld(reserve, work)
	}
	return b.WithReservation(reserve, work)
}

func MkdirAll(name string, mode os.FileMode) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	b := ForPath(name)
	if b == nil {
		return os.MkdirAll(name, mode)
	}
	relative, err := filepath.Rel(b.root, name)
	if err != nil || !filepath.IsLocal(relative) {
		return errors.New("invalid managed directory path")
	}
	root, err := os.OpenRoot(b.root)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Stat(relative); err == nil && info.IsDir() {
		return nil
	}
	return b.WithReservation(Reservation{Entries: int64(strings.Count(filepath.ToSlash(relative), "/")) + 1}, func() error { return root.MkdirAll(relative, mode) })
}
func TempDir(parent, prefix string) (string, error) {
	if err := ValidatePath(parent); err != nil {
		return "", err
	}
	if err := ValidatePath(prefix); err != nil {
		return "", err
	}
	b := ForPath(parent)
	if b == nil {
		return os.MkdirTemp(parent, prefix)
	}
	var name string
	err := b.WithReservation(Reservation{Entries: int64(strings.Count(filepath.ToSlash(parent), "/")) + 1}, func() error {
		relative, err := filepath.Rel(b.root, parent)
		if err != nil || !filepath.IsLocal(relative) {
			return errors.New("invalid temporary directory root")
		}
		root, err := os.OpenRoot(b.root)
		if err != nil {
			return err
		}
		defer root.Close()
		if err = root.MkdirAll(relative, 0700); err != nil {
			return err
		}
		relative = filepath.Join(relative, prefix+randomID())
		if err = root.Mkdir(relative, 0700); err != nil {
			return err
		}
		name = filepath.Join(b.root, relative)
		return nil
	})
	return name, err
}

func (f *BudgetFile) ReadFrom(source io.Reader) (int64, error) {
	return io.CopyBuffer(struct{ io.Writer }{f}, source, make([]byte, 64<<10))
}
func (f *BudgetFile) WriteString(value string) (int, error) { return f.Write([]byte(value)) }
func (f *BudgetFile) WriteAt(data []byte, offset int64) (n int, err error) {
	if offset < 0 {
		return 0, errors.New("negative file offset")
	}
	if f.bucket == nil {
		return f.File.WriteAt(data, offset)
	}
	if offset > int64(^uint64(0)>>1)-int64(len(data)) {
		return 0, ErrQuota
	}
	err = f.bucket.WithReservation(Reservation{Bytes: offset + int64(len(data))}, func() error { var e error; n, e = f.File.WriteAt(data, offset); return e })
	return
}
func (f *BudgetFile) Truncate(size int64) error {
	if f.bucket == nil {
		return f.File.Truncate(size)
	}
	return f.bucket.WithReservationUnheld(Reservation{}, func() error {
		info, err := f.File.Stat()
		if err != nil {
			return err
		}
		if size < 0 || size > info.Size() {
			return errors.New("growing files with Truncate bypasses admission; write bounded bytes instead")
		}
		return f.File.Truncate(size)
	})
}

// MoveUnique publishes a fully staged upload without overwriting a concurrent
// UI write. Source and destination must be in the same admitted storage bucket.
func MoveUnique(source, directory, name string) (destination string, err error) {
	for _, path := range []string{source, directory, name} {
		if err := ValidatePath(path); err != nil {
			return "", err
		}
	}
	b := ForPath(directory)
	if b == nil || ForPath(source) != b {
		return "", errors.New("upload staging requires one configured workspace bucket")
	}
	err = b.WithReservation(Reservation{}, func() error {
		root, err := os.OpenRoot(b.root)
		if err != nil {
			return err
		}
		defer root.Close()
		from, err := filepath.Rel(b.root, source)
		if err != nil || !filepath.IsLocal(from) {
			return errors.New("invalid upload staging path")
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		extension := filepath.Ext(name)
		for index := 0; index < 100000; index++ {
			candidate := name
			if index > 0 {
				candidate = fmt.Sprintf("%s-%d%s", stem, index, extension)
			}
			target := filepath.Join(directory, candidate)
			relative, err := filepath.Rel(b.root, target)
			if err != nil || !filepath.IsLocal(relative) {
				return errors.New("invalid upload destination")
			}
			if _, err = root.Lstat(relative); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
			if err = root.Rename(from, relative); err != nil {
				return err
			}
			destination = target
			return nil
		}
		return errors.New("too many conflicting upload filenames")
	})
	return
}

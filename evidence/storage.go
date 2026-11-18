package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/storage"
	"io"
	"os"
	"path/filepath"
	"time"
)

func verifyRootBody(root *os.Root, name, hash string, length int64) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != length {
		return errors.New("evidence body is not a regular file of the expected length")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, length+1))
	if err != nil {
		return err
	}
	if n != length || hex.EncodeToString(h.Sum(nil)) != hash {
		return errors.New("evidence body checksum mismatch")
	}
	return nil
}
func (s *Store) writeQuotaBody(reader io.Reader, length int64, expectedHash string) (string, error) {
	config, err := storage.LoadConfig()
	if err != nil {
		return "", err
	}
	if length < 0 || length > config.MaxBodyBytes {
		return "", &storage.QuotaError{Kind: storage.Evidence, Detail: fmt.Sprintf("declared body length must be between 0 and %d bytes", config.MaxBodyBytes)}
	}
	bucket, err := storage.Ensure(s.Dir, storage.Evidence)
	if err != nil {
		return "", err
	}
	if expectedHash != "" {
		target, err := hashPath(s.Dir, expectedHash)
		if err != nil {
			return "", err
		}
		relative, _ := filepath.Rel(s.Dir, target)
		found := false
		err = bucket.WithReservation(storage.Reservation{}, func() error {
			root, err := os.OpenRoot(s.Dir)
			if err != nil {
				return err
			}
			defer root.Close()
			if _, err = root.Lstat(relative); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			if err = verifyRootBody(root, relative, expectedHash, length); err != nil {
				return err
			}
			found = true
			now := time.Now()
			return root.Chtimes(relative, now, now)
		})
		if err != nil {
			return "", err
		}
		if found {
			return expectedHash, nil
		}
	}
	var result string
	err = bucket.WithReservation(storage.Reservation{Bytes: length, Entries: 5}, func() error {
		root, err := os.OpenRoot(s.Dir)
		if err != nil {
			return err
		}
		defer root.Close()
		if err = root.MkdirAll(".staging", 0700); err != nil {
			return err
		}
		temporary := ".staging/body-" + fmt.Sprintf("%d", time.Now().UnixNano())
		f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer func() { f.Close(); _ = root.Remove(temporary) }()
		h := sha256.New()
		n, err := io.CopyN(io.MultiWriter(f, h), reader, length)
		if err != nil {
			return fmt.Errorf("incomplete evidence body: expected %d bytes, read %d: %w", length, n, err)
		}
		var extra [1]byte
		if n, readErr := io.ReadFull(reader, extra[:]); n != 0 || readErr != io.EOF {
			return errors.New("evidence body exceeds its declared length")
		}
		result = hex.EncodeToString(h.Sum(nil))
		if expectedHash != "" && expectedHash != result {
			return errors.New("original traffic body hash mismatch")
		}
		if err = f.Sync(); err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		target, _ := hashPath(s.Dir, result)
		relative, _ := filepath.Rel(s.Dir, target)
		if _, err = root.Lstat(relative); err == nil {
			if err = verifyRootBody(root, relative, result, length); err != nil {
				return err
			}
			now := time.Now()
			return root.Chtimes(relative, now, now)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err = root.MkdirAll(filepath.Dir(relative), 0700); err != nil {
			return err
		}
		if err = root.Rename(temporary, relative); err != nil {
			return err
		}
		directory, err := root.Open(filepath.Dir(relative))
		if err != nil {
			return err
		}
		defer directory.Close()
		return directory.Sync()
	})
	return result, err
}

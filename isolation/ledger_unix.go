//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package isolation

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Reserve identities before creating a worker. On restart all reserved identities
// are closed, including those whose API client vanished with an active process.
// Never evict tombstones: maintenance must stop both services, rotate the shared
// broker token and reset the ledger before starting fresh API orchestration.
func (b *Broker) openLedger(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("an absolute broker-only worker state directory is required")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("worker state must be a private directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("worker state directory must have mode 0700")
	}
	name := filepath.Join(dir, "runs-"+b.owner()+".log")
	fd, err := syscall.Open(name, syscall.O_RDWR|syscall.O_APPEND|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	fail := func(err error) error { _ = file.Close(); return err }
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(errors.New("worker state is already in use by another broker"))
	}
	info, err = file.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fail(errors.New("worker ledger must be a private regular file"))
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8<<20))
	if err != nil {
		return fail(err)
	}
	if len(raw) >= 8<<20 || len(raw) > 0 && raw[len(raw)-1] != '\n' {
		return fail(errors.New("worker run ledger is oversized or incomplete; operator recovery required"))
	}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		session, id, ok := strings.Cut(string(line), "/")
		if !ok || !sessionPattern.MatchString(session) || !validRunID(id) || b.known[string(line)] {
			return fail(errors.New("invalid worker run ledger; operator recovery required"))
		}
		if len(b.known) >= maxRunLedger {
			return fail(fmt.Errorf("worker run ledger exceeds %d identities", maxRunLedger))
		}
		b.known[string(line)] = true
		b.closed[string(line)] = true
	}
	if err = file.Sync(); err != nil {
		return fail(err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fail(err)
	}
	err = directory.Sync()
	_ = directory.Close()
	if err != nil {
		return fail(err)
	}
	b.ledger = file
	return nil
}

package server

import (
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/storage"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const maxStagedUploadBytes int64 = 4 << 20
const maxStagedUploadFiles = 1000

// stagedUploadSnapshot is request-local. Its only source is the explicit draft_id,
// never a pathname found in user prose. Immutable bytes survive later draft edits.
type stagedUploadSnapshot struct {
	files          map[string][]byte
	absolutePrefix string
}

// openExactUploadDir refuses symlinks, including links to a different directory
// inside the data root. Comparing the opened directory identity closes a
// replacement race between Lstat and OpenRoot without reading the substituted tree.
func openExactUploadDir(parent *os.Root, name string, create bool) (*os.Root, error) {
	if create {
		if err := parent.Mkdir(name, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("attachment directory is not an ordinary directory")
	}
	opened, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := opened.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		opened.Close()
		return nil, errors.New("attachment directory changed while opening")
	}
	return opened, nil
}

func readStagedUploads(dataDir, draftID string) (*stagedUploadSnapshot, error) {
	if draftID == "" {
		return nil, nil
	}
	if len(draftID) > 128 || !safeChatID.MatchString(draftID) {
		return nil, errors.New("invalid draft_id")
	}
	prefix, err := filepath.Abs(filepath.Join(dataDir, "drafts", draftID, "uploads"))
	if err != nil {
		return nil, err
	}
	snapshot := &stagedUploadSnapshot{files: map[string][]byte{}, absolutePrefix: prefix}
	base, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	drafts, err := openExactUploadDir(base, "drafts", false)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return nil, err
	}
	defer drafts.Close()
	draft, err := openExactUploadDir(drafts, draftID, false)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return nil, err
	}
	defer draft.Close()
	uploads, err := openExactUploadDir(draft, "uploads", false)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return nil, err
	}
	defer uploads.Close()
	directory, err := uploads.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(maxStagedUploadFiles + 1)
	closeErr := directory.Close()
	if readErr != nil && readErr != io.EOF {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > maxStagedUploadFiles {
		return nil, fmt.Errorf("staged attachments exceed %d files", maxStagedUploadFiles)
	}
	var used int64
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			return nil, errors.New("invalid staged attachment filename")
		}
		before, err := uploads.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !before.Mode().IsRegular() {
			return nil, errors.New("staged attachments must be regular files without symlinks or subdirectories")
		}
		if before.Size() > maxStagedUploadBytes-used {
			return nil, errors.New("staged attachments exceed the 4 MiB isolated transfer limit")
		}
		// NONBLOCK prevents a raced-in FIFO from hanging the request before f.Stat.
		f, err := uploads.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		actual, statErr := f.Stat()
		if statErr != nil || !actual.Mode().IsRegular() || !os.SameFile(before, actual) {
			f.Close()
			return nil, errors.New("staged attachment changed while opening")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, maxStagedUploadBytes-used+1))
		closeErr := f.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		used += int64(len(data))
		if used > maxStagedUploadBytes {
			return nil, errors.New("staged attachments exceed the 4 MiB isolated transfer limit")
		}
		snapshot.files[name] = data
	}
	return snapshot, nil
}

func (s *stagedUploadSnapshot) rewrite(text string) (string, error) {
	if s == nil {
		return text, nil
	}
	prefix := s.absolutePrefix
	var out strings.Builder
	for {
		index := strings.Index(text, prefix)
		if index < 0 {
			out.WriteString(text)
			break
		}
		before := text[:index]
		after := text[index+len(prefix):]
		leftOkay := before == ""
		if !leftOkay {
			r, _ := utf8.DecodeLastRuneInString(before)
			leftOkay = unicode.IsSpace(r) || strings.ContainsRune("\"'`([{=:", r)
		}
		rightOkay := after == ""
		if !rightOkay {
			r, _ := utf8.DecodeRuneInString(after)
			rightOkay = r == '/' || r == '\\' || unicode.IsSpace(r) || strings.ContainsRune("\"'`)]}>,;:", r)
		}
		out.WriteString(before)
		if leftOkay && rightOkay {
			if len(s.files) == 0 {
				return "", errors.New("the referenced draft has no staged attachments")
			}
			out.WriteString("/workspace/uploads")
		} else {
			out.WriteString(prefix)
		}
		text = after
	}
	return out.String(), nil
}

// copyToTask runs before seeding or engine admission. Only a newly created task
// directory is owned by this copy; failures remove that directory without
// touching any pre-existing filesystem entry or the source draft.
func (s *stagedUploadSnapshot) copyToTaskRaw(dataDir, taskID string) error {
	if s == nil || len(s.files) == 0 {
		return nil
	}
	if canonical, ok := canonicalTaskID(taskID); !ok || canonical != taskID {
		return errors.New("invalid destination task ID")
	}
	base, err := os.OpenRoot(dataDir)
	if err != nil {
		return err
	}
	defer base.Close()
	tasks, err := openExactUploadDir(base, "tasks", true)
	if err != nil {
		return err
	}
	defer tasks.Close()
	if err = tasks.Mkdir(taskID, 0700); err != nil {
		return fmt.Errorf("create isolated task attachment directory: %w", err)
	}
	copied := false
	defer func() {
		if !copied {
			_ = tasks.RemoveAll(taskID)
		}
	}()
	task, err := openExactUploadDir(tasks, taskID, false)
	if err != nil {
		return err
	}
	defer task.Close()
	uploads, err := openExactUploadDir(task, "uploads", true)
	if err != nil {
		return err
	}
	defer uploads.Close()
	for name, data := range s.files {
		f, err := uploads.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	copied = true
	return nil
}

func (s *stagedUploadSnapshot) copyToTask(dataDir, taskID string) error {
	if s == nil || len(s.files) == 0 {
		return nil
	}
	reserve := storage.Reservation{Entries: int64(len(s.files)) + 3}
	for _, data := range s.files {
		reserve.Bytes += int64(len(data))
	}
	return storage.GuardUnheld(dataDir, reserve, func() error { return s.copyToTaskRaw(dataDir, taskID) })
}

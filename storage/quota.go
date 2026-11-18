// Package storage enforces conservative filesystem admission budgets. PostgreSQL,
// application logs and container image layers remain outside these budgets.
package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Kind string

const (
	Evidence  Kind = "evidence"
	Traffic   Kind = "traffic"
	Workspace Kind = "workspace"
)

var ErrQuota = errors.New("storage quota exceeded")
var ErrHold = errors.New("storage is protected by a legal hold")

type Limits struct {
	Bytes   int64 `json:"limit_bytes"`
	Entries int64 `json:"limit_entries"`
}
type Config struct {
	Evidence, Traffic, Workspace      Limits
	MaxBodyBytes, MaxTrafficExchanges int64
}

func positive(name string, fallback int64) (int64, error) {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback, nil
	}
	if raw == "" || strings.TrimSpace(raw) != raw || strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, fmt.Errorf("%s must be a positive decimal integer", name)
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive decimal integer", name)
	}
	return value, nil
}
func LoadConfig() (Config, error) {
	var c Config
	var err error
	for _, v := range []struct {
		name     string
		fallback int64
		target   *int64
	}{
		{"ARTEX_EVIDENCE_QUOTA_BYTES", 2 << 30, &c.Evidence.Bytes}, {"ARTEX_TRAFFIC_QUOTA_BYTES", 5 << 30, &c.Traffic.Bytes}, {"ARTEX_WORKSPACE_QUOTA_BYTES", 5 << 30, &c.Workspace.Bytes},
		{"ARTEX_EVIDENCE_QUOTA_ENTRIES", 100000, &c.Evidence.Entries}, {"ARTEX_TRAFFIC_QUOTA_ENTRIES", 100000, &c.Traffic.Entries}, {"ARTEX_WORKSPACE_QUOTA_ENTRIES", 100000, &c.Workspace.Entries},
		{"ARTEX_MAX_EVIDENCE_BODY_BYTES", 64 << 20, &c.MaxBodyBytes}, {"ARTEX_MAX_TRAFFIC_EXCHANGES", 100000, &c.MaxTrafficExchanges},
	} {
		if *v.target, err = positive(v.name, v.fallback); err != nil {
			return c, err
		}
	}
	for _, limit := range []Limits{c.Evidence, c.Traffic, c.Workspace} {
		if limit.Entries > 1000000 {
			return c, errors.New("storage entry quotas must not exceed 1000000")
		}
	}
	if c.MaxBodyBytes > c.Evidence.Bytes {
		return c, errors.New("ARTEX_MAX_EVIDENCE_BODY_BYTES must fit the evidence quota")
	}
	if c.Traffic.Bytes < 1<<20 {
		return c, errors.New("traffic quota must be at least 1 MiB for its bounded SQLite index")
	}
	return c, nil
}
func (c Config) For(kind Kind) Limits {
	switch kind {
	case Evidence:
		return c.Evidence
	case Traffic:
		return c.Traffic
	default:
		return c.Workspace
	}
}

type Usage struct {
	Bytes   int64 `json:"bytes"`
	Entries int64 `json:"entries"`
}
type Reservation = Usage
type Status struct {
	Kind Kind `json:"kind"`
	Limits
	Usage
	ReadOnly bool   `json:"read_only"`
	Holds    []Hold `json:"holds"`
	Error    string `json:"error,omitempty"`
}
type QuotaError struct {
	Kind   Kind
	Detail string
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("%s storage admission refused: %s; delete unheld data or increase the reviewed storage quota", e.Kind, e.Detail)
}
func (e *QuotaError) Unwrap() error { return ErrQuota }

type Bucket struct {
	root    string
	kind    Kind
	limits  Limits
	exclude map[string]bool
	mu      sync.Mutex
}

var registry = struct {
	sync.RWMutex
	buckets map[string]*Bucket
}{buckets: map[string]*Bucket{}}

func Open(root string, kind Kind, limits Limits, exclude ...string) (*Bucket, error) {
	if err := ValidatePath(root); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(absolute)
	registry.Lock()
	defer registry.Unlock()
	if old := registry.buckets[root]; old != nil {
		if old.kind != kind || old.limits != limits {
			return nil, errors.New("storage quota root already registered with different limits")
		}
		return old, nil
	}
	if limits.Bytes <= 0 || limits.Entries <= 0 {
		return nil, errors.New("invalid storage limits")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("storage root must be an ordinary directory")
	}
	b := &Bucket{root: root, kind: kind, limits: limits, exclude: map[string]bool{}}
	for _, name := range exclude {
		b.exclude[name] = true
	}
	registry.buckets[root] = b
	return b, nil
}

// ConfigureDataDir validates every setting even when capture is disabled.
func ConfigureDataDir(root string) (Config, error) {
	c, err := LoadConfig()
	if err != nil {
		return c, err
	}
	if err = ValidateRawTranscripts(root); err != nil {
		return c, err
	}
	if RawTranscriptsEnabled() {
		if c.Workspace.Bytes <= MaxRawTranscriptBytes {
			return c, errors.New("workspace quota must exceed the 256 MiB raw transcript headroom")
		}
		c.Workspace.Bytes -= MaxRawTranscriptBytes
		if c.Workspace.Entries <= MaxRawTranscriptInodes {
			return c, errors.New("workspace entry quota must exceed the 8192 raw transcript inode headroom")
		}
		c.Workspace.Entries -= MaxRawTranscriptInodes
	}
	for _, v := range []struct {
		path    string
		kind    Kind
		exclude []string
	}{{root, Workspace, []string{"evidence", "traffic"}}, {filepath.Join(root, "evidence"), Evidence, nil}, {filepath.Join(root, "traffic"), Traffic, nil}} {
		bucket, openErr := Open(v.path, v.kind, c.For(v.kind), v.exclude...)
		if openErr != nil {
			return c, openErr
		}
		if err = bucket.validateHolds(); err != nil {
			return c, err
		}
	}
	return c, nil
}
func ForPath(path string) *Bucket {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	registry.RLock()
	defer registry.RUnlock()
	var found *Bucket
	for root, b := range registry.buckets {
		relative, err := filepath.Rel(root, absolute)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && (found == nil || len(root) > len(found.root)) {
			found = b
		}
	}
	return found
}
func Ensure(root string, kind Kind) (*Bucket, error) {
	if b := ForPath(root); b != nil && b.kind == kind {
		return b, nil
	}
	c, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return Open(root, kind, c.For(kind))
}
func (b *Bucket) Root() string   { return b.root }
func (b *Bucket) Limits() Limits { return b.limits }
func (b *Bucket) scan(root *os.Root) (Usage, error) {
	var usage Usage
	err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if !strings.Contains(name, "/") && (b.exclude[name] || name == "_quota.lock" || name == "_storage-holds.json" || name == "_storage-holds.next") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		usage.Entries++
		if usage.Entries > 1000000 {
			return errors.New("storage recount exceeded the bounded one-million-entry scan; remove unneeded data before writing")
		}
		info, err := d.Info()
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("storage contains a symlink or special file; remove it before writing")
		}
		if info.Mode().IsRegular() {
			if info.Size() < 0 || info.Size() > int64(^uint64(0)>>1)-usage.Bytes {
				return errors.New("storage size overflow")
			}
			usage.Bytes += info.Size()
		}
		return nil
	})
	return usage, err
}
func (b *Bucket) exclusive(fn func(*os.Root) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	root, err := os.OpenRoot(b.root)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Lstat("_quota.lock"); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("invalid storage lock file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	lock, err := root.OpenFile("_quota.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = lockFile(lock); err != nil {
		return err
	}
	defer unlockFile(lock)
	return fn(root)
}

// WithReservation holds a process-wide and OS-wide lock until mutation finishes.
// The caller must reserve peak additional bytes/entries, including temporary
// copies and metadata, and must not acquire this same bucket recursively.
func (b *Bucket) WithReservation(reserve Reservation, fn func() error) error {
	return b.admit(reserve, false, fn)
}
func (b *Bucket) WithReservationUnheld(reserve Reservation, fn func() error) error {
	return b.admit(reserve, true, fn)
}
func (b *Bucket) admit(reserve Reservation, unheld bool, fn func() error) error {
	if reserve.Bytes < 0 || reserve.Entries < 0 {
		return errors.New("invalid negative storage reservation")
	}
	return b.exclusive(func(root *os.Root) error {
		if unheld {
			holds, err := readHolds(root)
			if err != nil {
				return err
			}
			if len(holds) > 0 {
				return ErrHold
			}
		}
		if reserve.Bytes == 0 && reserve.Entries == 0 {
			return fn()
		}
		used, err := b.scan(root)
		if err != nil {
			return &QuotaError{b.kind, err.Error()}
		}
		if reserve.Bytes > b.limits.Bytes-used.Bytes || reserve.Entries > b.limits.Entries-used.Entries {
			return &QuotaError{b.kind, fmt.Sprintf("used %d/%d bytes and %d/%d entries; operation needs at most %d bytes and %d entries", used.Bytes, b.limits.Bytes, used.Entries, b.limits.Entries, reserve.Bytes, reserve.Entries)}
		}
		return fn()
	})
}
func Guard(path string, reserve Reservation, fn func() error) error {
	if err := ValidatePath(path); err != nil {
		return err
	}
	if b := ForPath(path); b != nil {
		return b.WithReservation(reserve, fn)
	}
	return fn()
}
func (b *Bucket) WithUnheld(fn func() error) error {
	return b.exclusive(func(root *os.Root) error {
		holds, err := readHolds(root)
		if err != nil {
			return err
		}
		if len(holds) > 0 {
			return ErrHold
		}
		return fn()
	})
}
func (b *Bucket) Status() Status {
	out := Status{Kind: b.kind, Limits: b.limits}
	err := b.exclusive(func(root *os.Root) error {
		var err error
		out.Holds, err = readHolds(root)
		if err != nil {
			return err
		}
		out.Usage, err = b.scan(root)
		return err
	})
	if err != nil {
		out.Error = err.Error()
	}
	out.ReadOnly = err != nil || out.Usage.Bytes >= out.Limits.Bytes || out.Usage.Entries >= out.Limits.Entries
	return out
}
func StatusForDataDir(root string) []Status {
	var out []Status
	for _, path := range []string{filepath.Join(root, "evidence"), filepath.Join(root, "traffic"), root} {
		if b := ForPath(path); b != nil {
			out = append(out, b.Status())
		}
	}
	return out
}

type Hold struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func readHolds(root *os.Root) ([]Hold, error) {
	info, err := root.Lstat("_storage-holds.json")
	if os.IsNotExist(err) {
		return []Hold{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 128<<10 {
		return nil, errors.New("invalid legal-hold state; storage deletion is blocked")
	}
	file, err := root.Open("_storage-holds.json")
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, (128<<10)+1))
	closeErr := file.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(raw) > 128<<10 {
		return nil, errors.New("legal-hold state exceeds its metadata bound")
	}
	var state struct {
		Version int    `json:"version"`
		Holds   []Hold `json:"holds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || state.Version != 1 || state.Holds == nil || len(state.Holds) > 64 {
		return nil, errors.New("invalid legal-hold state; storage deletion is blocked")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("trailing legal-hold state data")
	}
	seen := map[string]bool{}
	for _, h := range state.Holds {
		if len(h.ID) != 32 || seen[h.ID] || len(h.Reason) == 0 || len(h.Reason) > 1000 || h.CreatedAt.IsZero() {
			return nil, errors.New("invalid legal-hold state; storage deletion is blocked")
		}
		if _, err := hex.DecodeString(h.ID); err != nil {
			return nil, err
		}
		seen[h.ID] = true
	}
	return state.Holds, nil
}
func writeHolds(root *os.Root, holds []Hold) error {
	raw, err := json.Marshal(struct {
		Version int    `json:"version"`
		Holds   []Hold `json:"holds"`
	}{1, holds})
	if err != nil {
		return err
	}
	if info, err := root.Lstat("_storage-holds.next"); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("invalid pending legal-hold state")
		}
		if err = root.Remove("_storage-holds.next"); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := root.OpenFile("_storage-holds.next", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err = root.Rename("_storage-holds.next", "_storage-holds.json"); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (b *Bucket) AddHold(reason string) (Hold, error) {
	var hold Hold
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1000 {
		return hold, errors.New("legal-hold reason must be 1-1000 UTF-8 bytes")
	}
	err := b.exclusive(func(root *os.Root) error {
		holds, err := readHolds(root)
		if err != nil {
			return err
		}
		if len(holds) >= 64 {
			return errors.New("at most 64 active legal holds are allowed per storage bucket")
		}
		var entropy [16]byte
		if _, err = rand.Read(entropy[:]); err != nil {
			return err
		}
		hold = Hold{hex.EncodeToString(entropy[:]), reason, time.Now().UTC()}
		return writeHolds(root, append(holds, hold))
	})
	return hold, err
}
func (b *Bucket) ReleaseHold(id string) error {
	return b.exclusive(func(root *os.Root) error {
		holds, err := readHolds(root)
		if err != nil {
			return err
		}
		out := holds[:0]
		found := false
		for _, h := range holds {
			if h.ID == id {
				found = true
				continue
			}
			out = append(out, h)
		}
		if !found {
			return os.ErrNotExist
		}
		return writeHolds(root, out)
	})
}

func randomID() string {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic("secure storage temporary filename unavailable")
	}
	return hex.EncodeToString(entropy[:])
}

// AcquireUnheld retains the same cross-process lock used by reservations. Delete
// stages release it only after commit/rollback, so GC cannot race an admitted
// content-addressed write in a second process.
func (b *Bucket) AcquireUnheld() (func(), error) {
	b.mu.Lock()
	root, err := os.OpenRoot(b.root)
	if err != nil {
		b.mu.Unlock()
		return nil, err
	}
	fail := func(err error) (func(), error) { root.Close(); b.mu.Unlock(); return nil, err }
	if info, err := root.Lstat("_quota.lock"); err == nil && !info.Mode().IsRegular() {
		return fail(errors.New("invalid storage lock file"))
	} else if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	file, err := root.OpenFile("_quota.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fail(err)
	}
	if err = lockFile(file); err != nil {
		file.Close()
		return fail(err)
	}
	release := sync.OnceFunc(func() { unlockFile(file); file.Close(); root.Close(); b.mu.Unlock() })
	holds, err := readHolds(root)
	if err != nil {
		release()
		return nil, err
	}
	if len(holds) > 0 {
		release()
		return nil, ErrHold
	}
	return release, nil
}
func CheckHold(path string) error {
	if b := ForPath(path); b != nil {
		return b.WithUnheld(func() error { return nil })
	}
	return nil
}

func GuardUnheld(path string, reserve Reservation, fn func() error) error {
	if err := ValidatePath(path); err != nil {
		return err
	}
	if b := ForPath(path); b != nil {
		return b.WithReservationUnheld(reserve, fn)
	}
	return fn()
}

func (b *Bucket) validateHolds() error {
	return b.exclusive(func(root *os.Root) error { _, err := readHolds(root); return err })
}

package traffic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Autumn-27/artex/storage"
	mproxy "github.com/lqqyt2423/go-mitmproxy/proxy"
)

const RawRetention = 30 * 24 * time.Hour

func (t *Traffic) storageBucket() (*storage.Bucket, error) {
	if t.quota != nil {
		return t.quota, nil
	}
	return storage.Ensure(t.dir, storage.Traffic)
}
func (t *Traffic) checkStorageHold() error {
	b, err := t.storageBucket()
	if err != nil {
		return err
	}
	return b.WithUnheld(func() error { return nil })
}
func (t *Traffic) AddStorageHold(reason string) (storage.Hold, error) {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	b, err := t.storageBucket()
	if err != nil {
		return storage.Hold{}, err
	}
	return b.AddHold(reason)
}
func (t *Traffic) ReleaseStorageHold(id string) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	b, err := t.storageBucket()
	if err != nil {
		return err
	}
	return b.ReleaseHold(id)
}
func (t *Traffic) CaptureError() string { t.wmu.Lock(); defer t.wmu.Unlock(); return t.captureError }
func (t *Traffic) sqlHeadroom() int64 {
	limit := t.indexBudget
	if limit == 0 {
		limit = 5 << 28
	}
	return 3*limit + (1 << 20)
}
func (t *Traffic) admitRows(additional int64) error {
	limit := t.maxExchanges
	if limit == 0 {
		limit = 100000
	}
	var count int64
	if err := t.db.QueryRow(`SELECT COUNT(*) FROM exchanges`).Scan(&count); err != nil {
		return err
	}
	if additional > limit-count {
		return &storage.QuotaError{Kind: storage.Traffic, Detail: fmt.Sprintf("capture metadata limit %d reached", limit)}
	}
	var pages, pageSize int64
	if err := t.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		return err
	}
	if err := t.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return err
	}
	if t.indexBudget > 0 && pages*pageSize > t.indexBudget {
		return &storage.QuotaError{Kind: storage.Traffic, Detail: "existing SQLite index exceeds its reserved page budget; purge unheld raw captures to reclaim"}
	}
	return nil
}
func (t *Traffic) record(f *mproxy.Flow) {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	err := t.admitRows(1)
	if err == nil && (len(f.Request.Body) > 64<<20 || len(f.Response.Body) > 64<<20) {
		err = &storage.QuotaError{Kind: storage.Traffic, Detail: "one captured request/response body exceeds 64 MiB"}
	}
	if err == nil {
		b, e := t.storageBucket()
		err = e
		if err == nil {
			reserve := storage.Reservation{Bytes: t.sqlHeadroom(), Entries: 12}
			for _, body := range [][]byte{f.Request.Body, f.Response.Body} {
				if len(body) > maxInlineBody {
					sum := sha256.Sum256(body)
					hash := hex.EncodeToString(sum[:])
					p := filepath.Join(t.dir, "_blobs", "sha256", hash[:2], hash+".bin")
					if _, e := os.Lstat(p); os.IsNotExist(e) {
						reserve.Bytes += int64(len(body))
					} else if e != nil {
						err = e
						break
					}
				}
			}
			if err == nil {
				err = b.WithReservation(reserve, func() error { return t.recordWithinQuota(f) })
			}
		}
	}
	if err != nil {
		t.captureError = err.Error()
		log.Printf("[traffic] capture rejected: %v", err)
		if f.Response != nil {
			f.Response.StatusCode = 507
			f.Response.Header.Set("Content-Type", "text/plain; charset=utf-8")
			f.Response.Body = []byte("Raw traffic capture could not be preserved: " + err.Error())
		}
		return
	}
	t.captureError = ""
}
func (t *Traffic) writeRawBlob(hash string, data []byte) error {
	if !blobHashRe.MatchString(hash) {
		return errors.New("invalid traffic blob hash")
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	relative := filepath.Join("_blobs", "sha256", hash[:2], hash+".bin")
	if info, err := root.Lstat(relative); err == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
			return errors.New("existing traffic blob is invalid")
		}
		f, err := root.Open(relative)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err = io.Copy(h, io.LimitReader(f, int64(len(data))+1)); err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != hash {
			return errors.New("existing traffic blob checksum mismatch")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = root.MkdirAll(filepath.Dir(relative), 0700); err != nil {
		return err
	}
	temporary := relative + ".writing"
	f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { f.Close(); _ = root.Remove(temporary) }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return root.Rename(temporary, relative)
}
func readArchiveFile(dir, name string, limit int64) ([]byte, error) {
	f, err := storage.OpenRegular(dir, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, &storage.QuotaError{Kind: storage.Workspace, Detail: "archive traffic member exceeds its bounded regular-file limit"}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, storage.ErrQuota
	}
	return data, err
}
func (t *Traffic) ImportArchive(dir string) (imported int64, err error) {
	if t == nil {
		return 0, nil
	}
	raw, err := readArchiveFile(dir, "traffic.json", 64<<20)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var snapshot ArchiveSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return 0, err
	}
	if snapshot.Version != 1 {
		return 0, errors.New("unsupported traffic archive version")
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	var missing int64
	now := time.Now().Unix()
	for _, item := range snapshot.Exchanges {
		if item.TS > now || item.TS < 0 {
			return 0, errors.New("archived traffic capture timestamps must not be negative or in the future")
		}
		if len(item.ReqHead) > 1<<20 || len(item.RespHead) > 1<<20 || len(item.ReqBody) > maxInlineBody || len(item.RespBody) > maxInlineBody || len(item.URL) > 1<<20 {
			return 0, errors.New("archived traffic metadata exceeds capture limits")
		}
		var exists int
		if err = t.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM exchanges WHERE id=?)`, item.ID).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			missing++
		}
	}
	kept := snapshot.Exchanges[:0]
	needed := map[string]bool{}
	missing = 0
	t.lastImportSkipped = 0
	for _, item := range snapshot.Exchanges {
		if item.TS < now-int64(RawRetention/time.Second) {
			t.lastImportSkipped++
			continue
		}
		kept = append(kept, item)
		if item.ReqBlob != "" {
			needed[item.ReqBlob] = true
		}
		if item.RespBlob != "" {
			needed[item.RespBlob] = true
		}
		var exists bool
		if err = t.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM exchanges WHERE id=?)`, item.ID).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			missing++
		}
	}
	snapshot.Exchanges = kept
	blobs := snapshot.Blobs[:0]
	for _, hash := range snapshot.Blobs {
		if needed[hash] {
			blobs = append(blobs, hash)
		}
	}
	snapshot.Blobs = blobs
	if err = t.admitRows(missing); err != nil {
		return 0, err
	}
	b, err := t.storageBucket()
	if err != nil {
		return 0, err
	}
	reserve := storage.Reservation{Bytes: t.sqlHeadroom(), Entries: 2}
	seen := map[string]bool{}
	for _, hash := range snapshot.Blobs {
		if !blobHashRe.MatchString(hash) || seen[hash] {
			return 0, errors.New("invalid or duplicate archived traffic blob")
		}
		seen[hash] = true
		data, err := readArchiveFile(dir, "blobs/"+hash+".bin", 64<<20)
		if err != nil {
			return 0, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != hash {
			return 0, errors.New("archived traffic blob checksum mismatch")
		}
		destination := filepath.Join(t.dir, "_blobs", "sha256", hash[:2], hash+".bin")
		if _, err = os.Lstat(destination); os.IsNotExist(err) {
			reserve.Bytes += int64(len(data))
			reserve.Entries += 4
		} else if err != nil {
			return 0, err
		}
	}
	for _, item := range snapshot.Exchanges {
		for _, hash := range []string{item.ReqBlob, item.RespBlob} {
			if hash != "" && !seen[hash] {
				return 0, errors.New("archived traffic references an absent blob")
			}
		}
	}
	err = b.WithReservation(reserve, func() error { var err error; imported, err = t.importArchiveWithinQuota(dir, snapshot); return err })
	return imported, err
}

// ExpireRaw deletes at most batch old captures and retries at most batch legacy
// directory removals. Finding-bound body copies live in the independent evidence
// store and are never touched. Legal holds block every destructive entry point.
func (t *Traffic) ExpireRaw(ctx context.Context, now time.Time, batch int) (int64, error) {
	if batch <= 0 || batch > 500 {
		return 0, errors.New("retention batch must be 1-500")
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	release, err := t.acquireStorageDeletion()
	if err != nil {
		return 0, err
	}
	defer release()
	if _, err := t.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS retention_deletions(path TEXT PRIMARY KEY)`); err != nil {
		return 0, err
	}
	if t.fts {
		if _, err = t.db.ExecContext(ctx, `DELETE FROM ex_fts WHERE rowid IN(SELECT rowid FROM ex_fts WHERE rowid NOT IN(SELECT rowid FROM exchanges) LIMIT ?)`, batch); err != nil {
			return 0, err
		}
	}
	if err := t.cleanExpiredLegacy(ctx, batch); err != nil {
		return 0, err
	}
	rows, err := t.db.QueryContext(ctx, `SELECT id,path FROM exchanges WHERE ts < ? ORDER BY ts,id LIMIT ?`, now.Add(-RawRetention).Unix(), batch)
	if err != nil {
		return 0, err
	}
	var ids []any
	var placeholders []string
	var legacy []string
	for rows.Next() {
		var id, path string
		if err = rows.Scan(&id, &path); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
		if path != "" {
			if !validLegacyRetentionPath(path) {
				rows.Close()
				return 0, errors.New("unsafe legacy traffic path blocks retention; inspect storage manually")
			}
			legacy = append(legacy, path)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, t.gcRawBatch(ctx, 1000)
	}
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, path := range legacy {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO retention_deletions(path) VALUES(?)`, path); err != nil {
			return 0, err
		}
	}
	deleted, err := t.deleteWhere(tx, "id IN ("+strings.Join(placeholders, ",")+")", ids...)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	if err = t.cleanExpiredLegacy(ctx, batch); err != nil {
		return deleted, err
	}
	if err = t.gcRawBatch(ctx, 1000); err != nil {
		return deleted, err
	}
	t.reclaim()
	return deleted, nil
}
func (t *Traffic) cleanExpiredLegacy(ctx context.Context, batch int) error {
	rows, err := t.db.QueryContext(ctx, `SELECT path FROM retention_deletions LIMIT ?`, batch)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, path)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range paths {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !validLegacyRetentionPath(path) {
			return errors.New("unsafe legacy retention cleanup path")
		}
		var refs int
		if err = t.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exchanges WHERE path=?`, path).Scan(&refs); err != nil {
			return err
		}
		if refs > 0 {
			return errors.New("legacy traffic directory still contains retained captures; raw cleanup requires operator review")
		}
		if err = root.RemoveAll(path); err != nil {
			return fmt.Errorf("legacy raw traffic remains pending removal: %w", err)
		}
		if _, err = t.db.ExecContext(ctx, `DELETE FROM retention_deletions WHERE path=?`, path); err != nil {
			return err
		}
	}
	return nil
}

func (t *Traffic) acquireStorageDeletion() (func(), error) {
	b, err := t.storageBucket()
	if err != nil {
		return nil, err
	}
	return b.AcquireUnheld()
}

func (t *Traffic) LastImportSkipped() int64 {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.lastImportSkipped
}

func validLegacyRetentionPath(value string) bool {
	clean := filepath.Clean(value)
	return filepath.IsLocal(value) && clean == value && clean != "." && !strings.HasPrefix(filepath.ToSlash(clean), "_") && !strings.HasPrefix(filepath.ToSlash(clean), ".")
}

func initializeTrafficDirectories(dir string, bucket *storage.Bucket, captureDisabled bool) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	_, indexErr := root.Lstat(filepath.Join("_index", "index.sqlite"))
	hasIndex := indexErr == nil
	for _, name := range []string{"_index", "_blobs", "_ca"} {
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			if captureDisabled && hasIndex && name != "_index" {
				continue
			}
			if err = bucket.WithReservation(storage.Reservation{Entries: 1}, func() error { return root.Mkdir(name, 0700) }); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("traffic internal directories must not be symlinks")
		}
	}
	for _, name := range []string{"index.sqlite", "index.sqlite-journal", "index.sqlite-wal", "index.sqlite-shm"} {
		info, err := root.Lstat(filepath.Join("_index", name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("traffic index and journal files must be ordinary files")
		}
	}
	return nil
}

// RetentionStatus exposes backlog and compatibility limits rather than claiming
// that expired bytes disappeared when an old layout needs operator review.
func (t *Traffic) RetentionStatus(now time.Time) map[string]any {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	out := map[string]any{"index_budget_bytes": t.indexBudget, "reserved_journal_headroom_bytes": t.sqlHeadroom(), "metadata_limit": t.maxExchanges}
	for key, query := range map[string]string{"expired_captures_pending": `SELECT COUNT(*) FROM exchanges WHERE ts < ?`, "legacy_rows_requiring_blob_gc_review": `SELECT COUNT(*) FROM exchanges WHERE path<>''`, "legacy_directories_pending_removal": `SELECT COUNT(*) FROM retention_deletions`} {
		var count int64
		var err error
		if key == "expired_captures_pending" {
			err = t.db.QueryRow(query, now.Add(-RawRetention).Unix()).Scan(&count)
		} else {
			err = t.db.QueryRow(query).Scan(&count)
		}
		if err == nil {
			out[key] = count
		}
	}
	root, err := os.OpenRoot(t.dir)
	if err == nil {
		defer root.Close()
		if directory, err := root.Open("_delete_staging"); err == nil {
			defer directory.Close()
			entries, _ := directory.ReadDir(1001)
			unknown := 0
			for _, entry := range entries {
				if entry.IsDir() {
					if _, err := root.Stat(filepath.Join("_delete_staging", entry.Name(), hostDeleteStageJournalName)); os.IsNotExist(err) {
						unknown++
					}
				}
			}
			out["legacy_raw_stages_requiring_review"] = unknown
		}
	}
	return out
}
func (t *Traffic) gcRawBatch(ctx context.Context, limit int) error {
	var legacy int64
	if err := t.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exchanges WHERE path<>''`).Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 {
		return errors.New("raw blob garbage collection deferred: legacy .http capture rows require operator review or explicit unheld purge; expired index rows were processed")
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	last := ""
	inspected := 0
	limited := false
	err = fs.WalkDir(root.FS(), "_blobs", func(name string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if name <= t.blobGCCursor {
			return nil
		}
		if inspected >= limit {
			limited = true
			return fs.SkipAll
		}
		inspected++
		last = name
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("raw blob GC found a symlink or special file; operator review required")
		}
		hash := strings.TrimSuffix(entry.Name(), ".bin")
		var referenced bool
		if err = t.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blob_refs WHERE hash=?)`, hash).Scan(&referenced); err != nil {
			return err
		}
		if referenced {
			return nil
		}
		if err = root.Remove(name); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if limited {
		t.blobGCCursor = last
	} else {
		t.blobGCCursor = ""
	}
	return nil
}

// initializeIndex reserves bounded schema/journal growth before SQLite creates a
// new database. Complete existing stores open without a growth reservation so
// operators can still read and reclaim an already-full bucket.
func (t *Traffic) initializeIndex() error {
	path := filepath.Join(t.dir, "_index", "index.sqlite")
	complete := false
	if info, err := os.Lstat(path); err == nil && info.Size() > 0 {
		var tables int
		err = t.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('exchanges','exchange_bodies','blob_refs')`).Scan(&tables)
		if err != nil {
			return err
		}
		complete = tables == 3
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if complete {
		var mode, fts int
		if err := t.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
			return err
		}
		t.incrementalVacuum = mode == autoVacuumIncremental
		if err := t.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ex_fts'`).Scan(&fts); err != nil {
			return err
		}
		t.fts = fts == 1
		return nil
	}
	return t.quota.WithReservation(storage.Reservation{Bytes: t.sqlHeadroom(), Entries: 4}, t.initIndex)
}

// The proxy dependency creates three small PEM files only when its primary CA
// file is absent. Validate all existing inputs and reserve that initial write;
// loading an existing CA must remain possible when capture admission is full.
func (t *Traffic) initializeCA(create func() error) error {
	missingPrimary := false
	for _, name := range []string{"mitmproxy-ca.pem", "mitmproxy-ca-cert.pem", "mitmproxy-ca-cert.cer"} {
		info, err := os.Lstat(filepath.Join(t.dir, "_ca", name))
		if os.IsNotExist(err) {
			if name == "mitmproxy-ca.pem" {
				missingPrimary = true
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<10 {
			return errors.New("traffic CA files must be bounded ordinary files")
		}
	}
	reserve := storage.Reservation{}
	if missingPrimary {
		reserve = storage.Reservation{Bytes: 192 << 10, Entries: 3}
	}
	return t.quota.WithReservation(reserve, create)
}

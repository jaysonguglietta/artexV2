package traffic

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Autumn-27/artex/storage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func retentionTraffic(t *testing.T) *Traffic {
	t.Helper()
	tr, err := OpenIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr
}
func TestRawRetentionDeletesOnlyExpiredRowsInBoundedBatches(t *testing.T) {
	tr := retentionTraffic(t)
	for _, path := range []string{"/old-a", "/old-b", "/recent"} {
		tr.record(newFlow("retention.example", "GET", path, nil, bytes.Repeat([]byte(path), 50000), withRespType("application/octet-stream")))
	}
	if err := tr.CaptureError(); err != "" {
		t.Fatal(err)
	}
	if _, err := tr.db.Exec(`UPDATE exchanges SET ts=? WHERE url LIKE '%/old-%'`, time.Now().Add(-31*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{2, 1} {
		deleted, err := tr.ExpireRaw(context.Background(), time.Now(), 1)
		if err != nil || deleted != 1 {
			t.Fatalf("batch deletion=%d err=%v", deleted, err)
		}
		if count, err := tr.Count(); err != nil || count != want {
			t.Fatalf("remaining=%d want=%d %v", count, want, err)
		}
	}
	var bodies, refs int
	if err := tr.db.QueryRow(`SELECT COUNT(*) FROM exchange_bodies`).Scan(&bodies); err != nil {
		t.Fatal(err)
	}
	if err := tr.db.QueryRow(`SELECT COUNT(DISTINCT exchange_id) FROM blob_refs`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if bodies != 1 || refs != 1 {
		t.Fatalf("expired body/reference metadata remained: %d %d", bodies, refs)
	}
}
func TestTrafficHoldBlocksEveryLegacyDeletionAndRetention(t *testing.T) {
	tr := retentionTraffic(t)
	tr.record(newFlow("held.example", "GET", "/", nil, []byte("held")))
	hold, err := tr.AddStorageHold("operator preservation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tr.DeleteHost("held.example"); !errors.Is(err, storage.ErrHold) {
		t.Fatal("host deletion bypassed hold", err)
	}
	if _, _, err = tr.DeleteAll(); !errors.Is(err, storage.ErrHold) {
		t.Fatal("purge bypassed hold", err)
	}
	if stage, err := tr.StageDeleteHostsExact([]string{"held.example"}); !errors.Is(err, storage.ErrHold) {
		if stage != nil {
			stage.Rollback()
		}
		t.Fatal("task deletion stage bypassed hold", err)
	}
	if _, err = tr.ExpireRaw(t.Context(), time.Now().Add(60*24*time.Hour), 500); !errors.Is(err, storage.ErrHold) {
		t.Fatal("expiry bypassed hold", err)
	}
	if err = tr.ReleaseStorageHold(hold.ID); err != nil {
		t.Fatal(err)
	}
	if n, _, err := tr.DeleteAll(); err != nil || n != 1 {
		t.Fatalf("unheld deletion failed: %d %v", n, err)
	}
}
func TestCaptureQuotaRejectsBeforeBlobOrSQLMutation(t *testing.T) {
	t.Setenv("ARTEX_TRAFFIC_QUOTA_BYTES", "8388608")
	tr := retentionTraffic(t)
	flow := newFlow("quota.example", "GET", "/large", nil, bytes.Repeat([]byte{'x'}, 2<<20), withRespType("application/octet-stream"))
	tr.record(flow)
	if flow.Response.StatusCode != 507 || tr.CaptureError() == "" {
		t.Fatal("capture quota failure was silent")
	}
	if count, err := tr.Count(); err != nil || count != 0 {
		t.Fatal("capture rows committed after rejection", count, err)
	}
	files, err := os.ReadDir(filepath.Join(tr.dir, "_blobs"))
	if err != nil || len(files) != 0 {
		t.Fatal("capture body written before quota admission", err)
	}
}
func TestTrafficMetadataQuotaAndEveryPooledConnectionPageCap(t *testing.T) {
	tr := retentionTraffic(t)
	tr.maxExchanges = 1
	tr.record(newFlow("quota.example", "GET", "/first", nil, []byte("first")))
	flow := newFlow("quota.example", "GET", "/second", nil, []byte("second"))
	tr.record(flow)
	if count, _ := tr.Count(); count != 1 || flow.Response.StatusCode != 507 {
		t.Fatal("metadata row cap bypassed")
	}
	one, err := tr.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := tr.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	for _, conn := range []*sql.Conn{one, two} {
		var cap, page int64
		if err = conn.QueryRowContext(t.Context(), `PRAGMA max_page_count`).Scan(&cap); err != nil {
			t.Fatal(err)
		}
		if err = conn.QueryRowContext(t.Context(), `PRAGMA page_size`).Scan(&page); err != nil {
			t.Fatal(err)
		}
		if cap*page > tr.indexBudget {
			t.Fatal("new pooled SQLite connection bypassed page budget")
		}
		var mode string
		if err = conn.QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "delete" {
			t.Fatal("unbounded WAL mode remained", mode, err)
		}
	}
}
func TestArchiveImportRejectsFutureAndSkipsExpiredRaw(t *testing.T) {
	tr := retentionTraffic(t)
	directory := t.TempDir()
	write := func(ts int64) {
		raw, _ := json.Marshal(ArchiveSnapshot{Version: 1, Exchanges: []ArchiveExchange{{ID: "archive-age", TS: ts, Host: "age.example", Method: "GET", URL: "https://age.example/"}}})
		if err := os.WriteFile(filepath.Join(directory, "traffic.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(time.Now().Add(time.Hour).Unix())
	if _, err := tr.ImportArchive(directory); err == nil {
		t.Fatal("future timestamp imported")
	}
	write(time.Now().Add(-31 * 24 * time.Hour).Unix())
	if count, err := tr.ImportArchive(directory); err != nil || count != 0 || tr.LastImportSkipped() != 1 {
		t.Fatalf("expired raw was reactivated: %d %v", count, err)
	}
	if count, _ := tr.Count(); count != 0 {
		t.Fatal("expired archive recreated raw capture")
	}
}

func TestTrafficStartupRejectsIndexSymlinkBeforeSQLiteWrites(t *testing.T) {
	directory, outside := t.TempDir(), t.TempDir()
	canary := filepath.Join(outside, "index.sqlite")
	if err := os.WriteFile(canary, []byte("outside-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "_index")); err != nil {
		t.Skip(err)
	}
	if tr, err := OpenIndex(directory); err == nil {
		tr.Close()
		t.Fatal("outside index directory was followed")
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "outside-canary" {
		t.Fatal("outside SQLite sentinel changed")
	}
}
func TestRetentionRemovesLegacyOrphanFTSRows(t *testing.T) {
	tr := retentionTraffic(t)
	if !tr.fts {
		t.Skip("FTS unavailable")
	}
	if _, err := tr.db.Exec(`INSERT INTO ex_fts(rowid,content) VALUES(999999,'legacy-orphan-secret')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.ExpireRaw(t.Context(), time.Now(), 500); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tr.db.QueryRow(`SELECT COUNT(*) FROM ex_fts WHERE rowid=999999`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired orphan full-text row survived", count, err)
	}
}

func TestTrafficStartupReservesBeforeCreatingIndex(t *testing.T) {
	t.Setenv("ARTEX_TRAFFIC_QUOTA_BYTES", "8388608")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "existing.bin"), bytes.Repeat([]byte{'x'}, 2<<20), 0600); err != nil {
		t.Fatal(err)
	}
	if tr, err := OpenIndex(directory); !errors.Is(err, storage.ErrQuota) {
		if tr != nil {
			tr.Close()
		}
		t.Fatalf("startup did not refuse unadmitted schema creation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "_index", "index.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("index created before reservation: %v", err)
	}
}
func TestTrafficExistingOverquotaIndexStillOpensForReadAndPurge(t *testing.T) {
	t.Setenv("ARTEX_TRAFFIC_QUOTA_BYTES", "8388608")
	directory := t.TempDir()
	tr, err := OpenIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tr.record(newFlow("reclaim.example", "GET", "/", nil, []byte("keep-readable")))
	if err := tr.CaptureError(); err != "" {
		t.Fatal(err)
	}
	tr.Close()
	if err = os.WriteFile(filepath.Join(directory, "existing.bin"), bytes.Repeat([]byte{'x'}, 9<<20), 0600); err != nil {
		t.Fatal(err)
	}
	tr, err = OpenIndex(directory)
	if err != nil {
		t.Fatal("overquota store cannot open to reclaim", err)
	}
	defer tr.Close()
	if count, err := tr.Count(); err != nil || count != 1 {
		t.Fatal("overquota store unreadable", count, err)
	}
	if count, _, err := tr.DeleteAll(); err != nil || count != 1 {
		t.Fatal("overquota store cannot purge", count, err)
	}
}

package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/storage"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type probeReader struct{ reads int }

func (r *probeReader) Read(value []byte) (int, error) { r.reads++; return 0, io.EOF }
func TestEvidenceRejectsDeclaredOversizeBeforeReadingOrCreatingStorage(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "evidence")
	store := New(nil, nil, directory)
	reader := new(probeReader)
	if _, err := store.writeBody(reader, (64<<20)+1, ""); !errors.Is(err, storage.ErrQuota) {
		t.Fatalf("oversized declaration not rejected: %v", err)
	}
	if reader.reads != 0 {
		t.Fatal("oversized reader was consumed")
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("oversized admission created storage before validation")
	}
}
func TestEvidenceRejectsActualOversizeAndKnownHashDedupeWorksWhenFull(t *testing.T) {
	t.Setenv("ARTEX_EVIDENCE_QUOTA_BYTES", "4")
	t.Setenv("ARTEX_MAX_EVIDENCE_BODY_BYTES", "4")
	store := New(nil, nil, t.TempDir())
	if _, err := store.writeBody(bytes.NewBufferString("longer"), 2, ""); err == nil {
		t.Fatal("reader larger than declaration accepted")
	}
	hash, err := store.writeBody(bytes.NewBufferString("keep"), 4, "")
	if err != nil {
		t.Fatal(err)
	}
	reader := new(probeReader)
	if got, err := store.writeBody(reader, 4, hash); err != nil || got != hash || reader.reads != 0 {
		t.Fatalf("known body required another staged copy: %s %v reads=%d", got, err, reader.reads)
	}
}
func TestEvidenceRootedReadsRejectOutsideBlobSymlinks(t *testing.T) {
	directory, outside := t.TempDir(), t.TempDir()
	body := []byte("outside evidence sentinel")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(outside, hash+".bin"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "blobs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "blobs", hash[:2])); err != nil {
		t.Skip(err)
	}
	store := New(nil, nil, directory)
	if file, _, err := store.OpenBody(db.TrafficEvidenceSnapshot{RespHash: hash, RespLen: int64(len(body))}, "response"); err == nil {
		file.Close()
		t.Fatal("outside body was opened")
	}
}
func TestEvidenceQuotaRollsBackEntireNewFindingAndBindings(t *testing.T) {
	t.Setenv("ARTEX_EVIDENCE_QUOTA_BYTES", "64")
	t.Setenv("ARTEX_MAX_EVIDENCE_BODY_BYTES", "64")
	store, input, _ := evidenceFixture(t)
	seedExchange(t, store, "quota-first", bytes.Repeat([]byte{'a'}, 40), false)
	first, err := store.Record(t.Context(), input, []db.TrafficRef{{TrafficID: "quota-first"}})
	if err != nil {
		t.Fatal(err)
	}
	seedExchange(t, store, "quota-second", bytes.Repeat([]byte{'b'}, 40), false)
	var before int
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM findings WHERE task_id=$1`, input.TaskID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Record(t.Context(), input, []db.TrafficRef{{TrafficID: "quota-second"}}); !errors.Is(err, storage.ErrQuota) {
		t.Fatalf("new finding exceeded quota: %v", err)
	}
	var after int
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM findings WHERE task_id=$1`, input.TaskID).Scan(&after); err != nil || after != before {
		t.Fatalf("partial finding committed: before=%d after=%d err=%v", before, after, err)
	}
	bindings, err := store.DB.GetFindingTraffic(t.Context(), first.FindingID)
	if err != nil || len(bindings.Bindings) != 1 {
		t.Fatal("prior finding evidence changed", err)
	}
}
func TestRawExpiryAndLegalHoldPreserveFindingEvidence(t *testing.T) {
	store, input, _ := evidenceFixture(t)
	body := bytes.Repeat([]byte("proof"), 100000)
	seedExchange(t, store, "old-linked", body, true)
	finding, err := store.Record(t.Context(), input, []db.TrafficRef{{TrafficID: "old-linked"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Traffic.DB().Exec(`UPDATE exchanges SET ts=? WHERE id='old-linked'`, time.Now().Add(-31*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.Traffic.ExpireRaw(context.Background(), time.Now(), 500); err != nil || deleted != 1 {
		t.Fatalf("raw expiry: %d %v", deleted, err)
	}
	snapshot := finding.Traffic.Bindings[0].Snapshot
	file, _, err := store.OpenBody(snapshot, "response")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("bound finding body was lost with raw capture")
	}
	bucket, err := storage.Ensure(store.Dir, storage.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	hold, err := bucket.AddHold("preserve unreferenced copy for review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.DeleteFinding(finding.FindingID); err != nil {
		t.Fatal(err)
	}
	if err = store.Collect(t.Context(), time.Now().Add(48*time.Hour)); !errors.Is(err, storage.ErrHold) {
		t.Fatal("legal hold did not block evidence GC", err)
	}
	if err = bucket.ReleaseHold(hold.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.PurgeUnreferenced(t.Context(), time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if file, _, err = store.OpenBody(snapshot, "response"); err == nil {
		file.Close()
		t.Fatal("explicit unreferenced purge did not reclaim the orphan")
	}
}

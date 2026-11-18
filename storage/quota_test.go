package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func testBucket(t *testing.T, bytes, entries int64) *Bucket {
	t.Helper()
	b, err := Open(t.TempDir(), Workspace, Limits{bytes, entries})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestQuotaReservationsSerializeConcurrentAtomicWriters(t *testing.T) {
	b := testBucket(t, 64, 100)
	var successful atomic.Int64
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			err := AtomicWrite(filepath.Join(b.Root(), string(rune('A'+index))), strings.NewReader("12345678"), 8, true)
			if err == nil {
				successful.Add(1)
			} else if !errors.Is(err, ErrQuota) {
				t.Errorf("unexpected writer error: %v", err)
			}
		}(index)
	}
	close(start)
	group.Wait()
	if successful.Load() != 8 {
		t.Fatalf("admitted %d writers; want eight", successful.Load())
	}
	status := b.Status()
	if status.Usage.Bytes != 64 || status.Usage.Entries != 8 {
		t.Fatalf("unexpected real-state usage: %+v", status)
	}
}
func TestAtomicQuotaFailuresPreserveOriginalAndRemoveTemporaryData(t *testing.T) {
	b := testBucket(t, 32, 100)
	target := filepath.Join(b.Root(), "original")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, strings.NewReader(strings.Repeat("x", 40)), 40, false); !errors.Is(err, ErrQuota) {
		t.Fatalf("expected pre-mutation quota failure, got %v", err)
	}
	if err := AtomicWrite(target, strings.NewReader("oversized"), 4, false); err == nil {
		t.Fatal("oversized reader accepted")
	}
	if err := AtomicWrite(target, strings.NewReader("x"), 4, false); !errors.Is(err, io.EOF) {
		t.Fatalf("short reader not rejected: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "keep" {
		t.Fatal("original data changed", string(got), err)
	}
	files, err := os.ReadDir(b.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.Contains(file.Name(), "writing") {
			t.Fatal("partial upload survived rejection")
		}
	}
}
func TestQuotaRecountsRealStateAndBoundsZeroByteFiles(t *testing.T) {
	b := testBucket(t, 16, 5)
	for index := 0; index < 20; index++ {
		err := AtomicWrite(filepath.Join(b.Root(), string(rune('a'+index))), strings.NewReader(""), 0, true)
		if err != nil && !errors.Is(err, ErrQuota) {
			t.Fatal(err)
		}
	}
	if used := b.Status().Usage.Entries; used > 5 || used < 1 {
		t.Fatalf("entry quota did not bound empty files: %d", used)
	}
	if err := os.WriteFile(filepath.Join(b.Root(), "outside-admission"), []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.WithReservation(Reservation{Bytes: 1}, func() error { t.Fatal("overquota writer ran"); return nil }); !errors.Is(err, ErrQuota) {
		t.Fatalf("real-state overflow was ignored: %v", err)
	}
	if err := b.WithUnheld(func() error { return os.Remove(filepath.Join(b.Root(), "outside-admission")) }); err != nil {
		t.Fatal("deletion blocked while full", err)
	}
}
func TestDurableHoldsBlockDestructionAndMalformedStateFailsClosed(t *testing.T) {
	b := testBucket(t, 1024, 100)
	hold, err := b.AddHold("preserve case evidence")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Status().Holds) != 1 {
		t.Fatal("hold not visible in real-state status")
	}
	if err = b.WithUnheld(func() error { t.Fatal("held deletion ran"); return nil }); !errors.Is(err, ErrHold) {
		t.Fatalf("hold bypass: %v", err)
	}
	if err = b.ReleaseHold(hold.ID); err != nil {
		t.Fatal(err)
	}
	if err = b.WithUnheld(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(b.Root(), "_storage-holds.json"), []byte(`{"version":1,"holdz":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = b.validateHolds(); err == nil {
		t.Fatal("misspelled hold state was treated as no holds")
	}
	if err = b.WithUnheld(func() error { t.Fatal("destruction ran with malformed hold state"); return nil }); err == nil {
		t.Fatal("malformed hold state did not fail closed")
	}
}
func TestQuotaNeverWritesThroughOutsideSymlinks(t *testing.T) {
	b := testBucket(t, 1024, 100)
	outside := t.TempDir()
	canary := filepath.Join(outside, "keep")
	if err := os.WriteFile(canary, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(b.Root(), "alias")); err != nil {
		t.Skip(err)
	}
	if err := AtomicWrite(filepath.Join(b.Root(), "alias", "keep"), strings.NewReader("overwritten"), 11, false); err == nil {
		t.Fatal("symlink mutation accepted")
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "keep" {
		t.Fatal("outside data changed")
	}
}
func TestConfigRejectsZeroNegativeWhitespaceAndOverflow(t *testing.T) {
	for _, value := range []string{"0", "-1", " 1024", "1024 ", "", "9999999999999999999999999999", "2GiB"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("ARTEX_EVIDENCE_QUOTA_BYTES", value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("malformed quota accepted")
			}
		})
	}
}
func TestBudgetFileCopyCannotBypassChunkAdmission(t *testing.T) {
	b := testBucket(t, 16, 100)
	file, err := CreateFile(filepath.Join(b.Root(), "archive.partial"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = io.Copy(file, strings.NewReader(strings.Repeat("x", 32))); !errors.Is(err, ErrQuota) {
		t.Fatalf("ReaderFrom bypassed quota: %v", err)
	}
	if _, err = file.WriteString(strings.Repeat("x", 32)); !errors.Is(err, ErrQuota) {
		t.Fatal("WriteString bypassed quota", err)
	}
	if info, err := file.Stat(); err != nil || info.Size() != 0 {
		t.Fatal("pre-admission rejection changed the stream", err)
	}
}
func TestDedupeAdmissionDoesNotRequireAdditionalCapacity(t *testing.T) {
	b := testBucket(t, 4, 100)
	target := filepath.Join(b.Root(), "body")
	if err := AtomicWrite(target, strings.NewReader("keep"), 4, true); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, strings.NewReader("keep"), 4, true); !errors.Is(err, os.ErrExist) {
		t.Fatalf("known existing file incorrectly required another copy: %v", err)
	}
}

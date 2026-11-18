package server

import (
	"bytes"
	"context"
	"encoding/json"
	pgdb "github.com/Autumn-27/artex/db"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestNewArchiveOmitsRawCapturesAndRestoresFindingEvidence(t *testing.T) {
	s, recorded, _ := trafficEvidenceServer(t)
	s.ctx = context.Background()
	proof := []byte("preserved linked evidence body")
	seedServerEvidenceFlow(t, s, "archive-linked-proof", proof)
	seedServerEvidenceFlow(t, s, "archive-unbound-raw", []byte("unbound raw must not enter the new archive"))
	bindings, err := s.evidenceStore().Bind(t.Context(), recorded.FindingID, []pgdb.TrafficRef{{TrafficID: "archive-linked-proof"}})
	if err != nil {
		t.Fatal(err)
	}
	finding, err := s.m.pg.GetFinding(recorded.FindingID)
	if err != nil || finding == nil || finding.TaskID == nil {
		t.Fatal("finding missing", err)
	}
	taskID := *finding.TaskID
	if _, err = s.m.Assets().UpsertRootDomain(pgdb.UpsertRootDomainReq{Domain: "evidence.local", TaskID: taskID}); err != nil {
		t.Fatal(err)
	}
	if err = s.m.SetTaskPaused(strconv.FormatInt(taskID, 10), true); err != nil {
		t.Fatal(err)
	}
	job, err := s.m.pg.QueueTaskArchive(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.runOneTaskArchiveJob(); err != nil {
		t.Fatal(err)
	}
	archived, err := s.m.pg.GetTaskArchive(job.ID)
	if err != nil || archived == nil || !archived.RawTrafficOmitted {
		t.Fatal("new archive omission indicator absent", err)
	}
	extracted := t.TempDir()
	if err = extractTaskArchivePackage(archived.ArchivePath, archived.SHA256, extracted); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(extracted, "traffic", "traffic.json")); !os.IsNotExist(err) {
		t.Fatal("unbound raw capture snapshot was archived")
	}
	raw, err := os.ReadFile(filepath.Join(extracted, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest pgdb.TaskArchiveSnapshot
	if err = json.Unmarshal(raw, &manifest); err != nil || !manifest.RawTrafficOmitted || manifest.DataCounts["traffic"] != 0 {
		t.Fatal("portable manifest misstates raw retention", err)
	}
	if _, err = s.m.pg.QueueTaskArchiveRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.runOneTaskArchiveJob(); err != nil {
		t.Fatal(err)
	}
	restored, err := s.m.pg.GetFindingTraffic(t.Context(), recorded.FindingID)
	if err != nil || len(restored.Bindings) != 1 {
		t.Fatal("finding evidence association was lost", err)
	}
	file, _, err := s.evidenceStore().OpenBody(bindings.Bindings[0].Snapshot, "response")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(got, proof) {
		t.Fatal("finding-linked archive evidence changed", err)
	}
}

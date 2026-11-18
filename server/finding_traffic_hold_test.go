package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/storage"
)

func TestEvidenceHoldRejectsAllBindingMutationsBeforeDatabaseAccess(t *testing.T) {
	dir := t.TempDir()
	bucket, err := storage.Ensure(filepath.Join(dir, "evidence"), storage.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	hold, err := bucket.AddHold("preserve binding association metadata")
	if err != nil {
		t.Fatal(err)
	}
	// No database is present: every mutation must fail at the hold boundary,
	// before querying or changing finding/binding metadata.
	s := &Server{m: &Manager{dir: dir}}
	for _, method := range []string{http.MethodPatch, http.MethodDelete, http.MethodPut} {
		request := httptest.NewRequest(method, "/api/exploration/findings/1/traffic/1", strings.NewReader(`{"version":1,"role":"proof","note":"changed","binding_ids":["1"]}`))
		response := httptest.NewRecorder()
		s.editFindingTraffic(response, request)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), storage.ErrHold.Error()) {
			t.Fatalf("%s bypassed evidence hold: status=%d body=%s", method, response.Code, response.Body.String())
		}
	}
	if holds := bucket.Status().Holds; len(holds) != 1 || holds[0].ID != hold.ID {
		t.Fatal("binding mutation changed legal-hold state")
	}
}

func TestEvidenceHoldRejectsReportReplacementBeforeDatabaseAccess(t *testing.T) {
	dir := t.TempDir()
	bucket, err := storage.Ensure(filepath.Join(dir, "evidence"), storage.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.AddHold("preserve finding report"); err != nil {
		t.Fatal(err)
	}
	s := &Server{m: &Manager{dir: dir}}
	result, err := s.toolUpdateFindingReport().Call(t.Context(), json.RawMessage(`{"finding_id":1,"report":"must not overwrite"}`), nil)
	if err != nil || !result.IsError || !strings.Contains(fmt.Sprint(result), storage.ErrHold.Error()) {
		t.Fatalf("held report write was not rejected: %+v %v", result, err)
	}
}

func TestEvidenceHoldPreservesReportAndAllowsNewFindings(t *testing.T) {
	s, recorded, request := trafficEvidenceServer(t)
	if _, err := s.m.pg.SetFindingReportVersionByNodeID(t.Context(), recorded.NodeID, "Retained original report", nil); err != nil {
		t.Fatal(err)
	}
	bucket, err := storage.Ensure(filepath.Join(s.m.dir, "evidence"), storage.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	hold, err := bucket.AddHold("preserve finding content")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bucket.ReleaseHold(hold.ID) })
	input, _ := json.Marshal(map[string]any{"finding_id": recorded.NodeID, "report": "Destructive replacement"})
	if result, err := s.toolUpdateFindingReport().Call(t.Context(), input, nil); err != nil || !result.IsError {
		t.Fatalf("held report replacement admitted: %+v %v", result, err)
	}
	if response := request(http.MethodDelete, fmt.Sprintf("/api/exploration/findings/%d", recorded.FindingID), ""); response.Code != http.StatusConflict {
		t.Fatalf("held finding deletion admitted: %d %s", response.Code, response.Body.String())
	}
	finding, err := s.m.pg.GetFinding(recorded.FindingID)
	if err != nil || finding == nil || finding.Report != "Retained original report" {
		t.Fatalf("held finding report was lost: %+v %v", finding, err)
	}
	var explorationID int64
	if err := s.m.pg.QueryRow(`SELECT exploration_id FROM exploration_nodes WHERE id=$1`, recorded.NodeID).Scan(&explorationID); err != nil {
		t.Fatal(err)
	}
	created, err := s.evidenceStore().Record(context.Background(), db.RecordFindingInput{
		TaskID: *finding.TaskID, ExplorationID: explorationID, Worker: "test", VulnClass: "TEST",
		Name: "New finding under hold", Summary: "Independent append remains permitted", Severity: "low",
	}, nil)
	if err != nil || created == nil || created.FindingID == recorded.FindingID {
		t.Fatalf("hold prevented new finding creation or overwrote the original: %+v %v", created, err)
	}
}

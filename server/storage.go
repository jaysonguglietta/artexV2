package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Autumn-27/artex/storage"
	"github.com/Autumn-27/norma/transcript"
)

func (m *Manager) checkEvidenceHold() error {
	return storage.CheckHold(filepath.Join(m.dir, "evidence"))
}
func (s *Server) rawTranscriptStore() *transcript.Store {
	if !storage.RawTranscriptsEnabled() {
		return nil
	}
	status := storage.RawTranscriptStatus(s.m.dir)
	if status.Error != "" {
		log.Printf("[raw-transcripts] disabled for this session: %s", status.Error)
		return nil
	}
	return transcript.NewStore(filepath.Join(s.m.dir, "transcripts"))
}
func (s *Server) registerStorage(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/storage", s.getStorageStatus)
	mux.HandleFunc("POST /api/storage/holds", s.addStorageHold)
	mux.HandleFunc("DELETE /api/storage/holds/{id}", s.releaseStorageHold)
	mux.HandleFunc("POST /api/storage/purge", s.purgeStorage)
}
func (s *Server) getStorageStatus(w http.ResponseWriter, r *http.Request) {
	result := map[string]any{"buckets": storage.StatusForDataDir(s.m.dir), "raw_retention_days": 30, "raw_transcripts": storage.RawTranscriptStatus(s.m.dir), "scope": "Filesystem admission only: PostgreSQL, its WAL, application logs and Docker image layers require separate capacity controls."}
	if s.m.traffic != nil {
		result["capture_error"] = s.m.traffic.CaptureError()
		result["raw_retention"] = s.m.traffic.RetentionStatus(time.Now())
	}
	if s.m.pg != nil {
		var legacy int64
		if err := s.m.pg.QueryRow(`SELECT COUNT(*) FROM task_archives WHERE COALESCE(archive_path,'')<>'' AND COALESCE(data_counts->>'raw_traffic_omitted','0')<>'1'`).Scan(&legacy); err == nil {
			result["legacy_archives_requiring_raw_retention_review"] = legacy
		}
	}
	s.m.storageStateMu.Lock()
	result["maintenance_error"] = s.m.storageMaintenanceError
	s.m.storageStateMu.Unlock()
	writeJSON(w, 200, result)
}
func (s *Server) storageBucket(kind string) (*storage.Bucket, error) {
	switch storage.Kind(kind) {
	case storage.Evidence:
		return storage.Ensure(filepath.Join(s.m.dir, "evidence"), storage.Evidence)
	case storage.Traffic:
		return storage.Ensure(filepath.Join(s.m.dir, "traffic"), storage.Traffic)
	case storage.Workspace:
		if b := storage.ForPath(s.m.dir); b != nil {
			return b, nil
		}
	}
	return nil, errors.New("kind must be evidence, traffic or workspace")
}
func storageRequest(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing storage request data")
	}
	return nil
}
func (s *Server) addStorageHold(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if err := storageRequest(w, r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	bucket, err := s.storageBucket(body.Kind)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.m.storageHoldMu.Lock()
	defer s.m.storageHoldMu.Unlock()
	var hold storage.Hold
	if body.Kind == "traffic" && s.m.traffic != nil {
		hold, err = s.m.traffic.AddStorageHold(body.Reason)
	} else if body.Kind == "evidence" {
		err = s.m.pg.WithEvidenceTx(r.Context(), func(*sql.Tx) error { var e error; hold, e = bucket.AddHold(body.Reason); return e })
	} else {
		hold, err = bucket.AddHold(body.Reason)
	}
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	log.Printf("[storage] operator added %s legal hold %s", body.Kind, hold.ID)
	writeJSON(w, 201, hold)
}
func (s *Server) releaseStorageHold(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind         string `json:"kind"`
		Confirmation string `json:"confirmation"`
	}
	if err := storageRequest(w, r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.Confirmation != "RELEASE LEGAL HOLD" {
		writeErr(w, 400, "confirmation must be RELEASE LEGAL HOLD")
		return
	}
	bucket, err := s.storageBucket(body.Kind)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.m.storageHoldMu.Lock()
	defer s.m.storageHoldMu.Unlock()
	if body.Kind == "traffic" && s.m.traffic != nil {
		err = s.m.traffic.ReleaseStorageHold(r.PathValue("id"))
	} else {
		err = bucket.ReleaseHold(r.PathValue("id"))
	}
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	log.Printf("[storage] operator released %s legal hold %s", body.Kind, r.PathValue("id"))
	writeJSON(w, 200, map[string]any{"released": true})
}
func (s *Server) purgeStorage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind         string `json:"kind"`
		Confirmation string `json:"confirmation"`
	}
	if err := storageRequest(w, r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.m.storageHoldMu.RLock()
	defer s.m.storageHoldMu.RUnlock()
	switch body.Kind {
	case "traffic":
		if body.Confirmation != "DELETE UNHELD RAW CAPTURES" {
			writeErr(w, 400, "confirmation must be DELETE UNHELD RAW CAPTURES")
			return
		}
		if s.m.traffic == nil {
			writeErr(w, 409, "traffic storage is not open")
			return
		}
		deleted, reclaimed, err := s.m.traffic.DeleteAll()
		if err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		log.Printf("[storage] operator purged %d raw captures", deleted)
		writeJSON(w, 200, map[string]any{"deleted": deleted, "reclaimed_bytes": reclaimed, "finding_evidence_preserved": true})
	case "evidence":
		if body.Confirmation != "PURGE UNREFERENCED EVIDENCE" {
			writeErr(w, 400, "confirmation must be PURGE UNREFERENCED EVIDENCE")
			return
		}
		if err := s.evidenceStore().PurgeUnreferenced(r.Context(), time.Now()); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"collected": true, "finding_evidence_preserved": true, "bounded_batch": true})
	default:
		writeErr(w, 400, "purge kind must be traffic or evidence; use workspace file deletion for explicit unheld artifacts")
	}
}
func (s *Server) runStorageMaintenance(maintenanceCtx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	traceTicker := time.NewTicker(30 * time.Second)
	defer traceTicker.Stop()
	lastTraceError := ""
	run := func() {
		var failures []string
		ctx, cancel := context.WithTimeout(maintenanceCtx, 30*time.Second)
		defer cancel()
		if err := s.evidenceStore().Collect(ctx, time.Now()); err != nil && !errors.Is(err, storage.ErrHold) && !errors.Is(err, context.Canceled) {
			log.Printf("[storage] evidence cleanup failed: %v", err)
			failures = append(failures, err.Error())
		}
		if s.m.traffic != nil {
			if _, err := s.m.traffic.ExpireRaw(ctx, time.Now(), 500); err != nil && !errors.Is(err, storage.ErrHold) && !errors.Is(err, context.Canceled) {
				log.Printf("[storage] raw capture retention failed: %v", err)
				failures = append(failures, err.Error())
			}
		}
		s.m.storageStateMu.Lock()
		s.m.storageMaintenanceError = strings.Join(failures, "; ")
		s.m.storageStateMu.Unlock()
	}
	run()
	for {
		select {
		case <-maintenanceCtx.Done():
			return
		case <-ticker.C:
			run()
		case <-traceTicker.C:
			status := storage.RawTranscriptStatus(s.m.dir)
			if status.Error != "" && status.Error != lastTraceError {
				log.Printf("[raw-transcripts] %s", status.Error)
			}
			lastTraceError = status.Error
		}
	}
}

func trafficStorageNeeded(dir, proxyAddr string) (bool, error) {
	if proxyAddr != "" {
		return true, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false, err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer file.Close()
	entries, readErr := file.ReadDir(4)
	if readErr != nil && readErr != io.EOF {
		return false, readErr
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "_quota.lock", "_storage-holds.json", "_storage-holds.next":
		default:
			return true, nil
		}
	}
	return false, nil
}

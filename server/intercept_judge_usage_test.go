package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// Coverage for the restored judge-usage endpoint: aggregate judge token totals/day buckets against PostgreSQL and return an empty series gracefully when no database is connected.
const judgeFixtureModel = "judge-usage-test-fixture"

// judgeUsageRig opens the shared test database and registers fixture cleanup.
func judgeUsageRig(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skipf("no database config (%v) — skipping", err)
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	// EnsureLLMUsageTable, rather than schema.sql, creates this table as in existing database fixtures.
	if err := d.EnsureLLMUsageTable(); err != nil {
		t.Fatalf("EnsureLLMUsageTable failed: %v", err)
	}
	cleanup := func() {
		_, _ = d.Exec(`DELETE FROM llm_usage WHERE model = $1`, judgeFixtureModel)
	}
	cleanup()
	t.Cleanup(cleanup)
	return &Server{m: &Manager{pg: d}}, d
}

// callJudgeUsage invokes the handler directly (auth is covered by auth tests).
func callJudgeUsage(t *testing.T, s *Server, query string) (int, db.JudgeUsage) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/intercept/judge/usage"+query, nil)
	s.interceptJudgeUsage(rec, req)
	var out db.JudgeUsage
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("Response JSON parsing failed: %v (body=%s)", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

// insertUsage writes one ledger row. tsShiftDays > 0 backdates it for the window test.
func insertUsage(t *testing.T, d *db.DB, worker string, in, out, cacheRead, cacheWrite int, tsShiftDays int) {
	t.Helper()
	_, err := d.Exec(`
INSERT INTO llm_usage(worker, model, profile_name, input_tokens, output_tokens, cache_read, cache_write, status, ts)
VALUES ($1,$2,'fixture',$3,$4,$5,$6,'ok', now() - ($7 * interval '1 day'))`,
		worker, judgeFixtureModel, in, out, cacheRead, cacheWrite, tsShiftDays)
	if err != nil {
		t.Fatalf("Fixture insert failed: %v", err)
	}
}

// Count only worker=judge rows in totals and daily buckets; other roles must not affect any value.
func TestInterceptJudgeUsageAggregates(t *testing.T) {
	s, d := judgeUsageRig(t)

	base, err := d.JudgeUsageStats(30)
	if err != nil {
		t.Fatalf("Baseline JudgeUsageStats failed: %v", err)
	}

	insertUsage(t, d, "judge", 100, 20, 7, 3, 0)
	insertUsage(t, d, "judge", 50, 10, 0, 0, 0)
	insertUsage(t, d, "worker", 999, 999, 999, 999, 0) // Must not contribute to judge aggregates.

	code, got := callJudgeUsage(t, s, "")
	if code != http.StatusOK {
		t.Fatalf("status=%d want 200 (body=%s)", code, "")
	}
	if got.Calls-base.Calls != 2 {
		t.Errorf("calls delta=%d want 2", got.Calls-base.Calls)
	}
	if got.InputTokens-base.InputTokens != 150 {
		t.Errorf("input delta=%d want 150 (non-judge rows may have leaked in)", got.InputTokens-base.InputTokens)
	}
	if got.OutputTokens-base.OutputTokens != 30 {
		t.Errorf("output delta=%d want 30", got.OutputTokens-base.OutputTokens)
	}
	if got.CacheReadTokens-base.CacheReadTokens != 7 {
		t.Errorf("cache_read delta=%d want 7", got.CacheReadTokens-base.CacheReadTokens)
	}
	if got.CacheWriteTokens-base.CacheWriteTokens != 3 {
		t.Errorf("cache_write delta=%d want 3", got.CacheWriteTokens-base.CacheWriteTokens)
	}

	today := time.Now().UTC().Format("2006-01-02")
	var found bool
	for _, day := range got.Daily {
		if day.Date == today {
			found = true
			if day.Calls < 2 {
				t.Errorf("Today bucket calls=%d want >=2", day.Calls)
			}
			if day.InputTokens < 150 {
				t.Errorf("Today bucket input=%d want >=150", day.InputTokens)
			}
		}
	}
	if !found {
		t.Errorf("Daily series lacks today (%s): %+v", today, got.Daily)
	}
}

// The days window narrows only the daily series; totals remain all-time.
func TestInterceptJudgeUsageDaysWindow(t *testing.T) {
	s, d := judgeUsageRig(t)

	insertUsage(t, d, "judge", 11, 1, 0, 0, 3) // Outside the window: three days ago.
	insertUsage(t, d, "judge", 22, 2, 0, 0, 0) // Inside the requested window.

	_, recent := callJudgeUsage(t, s, "?days=1")
	cutoff := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	for _, day := range recent.Daily {
		if day.Date < cutoff {
			t.Errorf("days=1 returned an out-of-window bucket (%s), cutoff=%s", day.Date, cutoff)
		}
	}
	if recent.Calls < 2 {
		t.Errorf("Total calls=%d want >=2 (totals are independent of the window)", recent.Calls)
	}

	_, all := callJudgeUsage(t, s, "?days=0") // Zero normalizes to the default 30-day window.
	if len(all.Daily) > 31 {
		t.Errorf("Unexpected bucket count after days=0 normalization: %d", len(all.Daily))
	}
}

// Without PostgreSQL, return HTTP 200 and an empty series so disabled-judge settings remain usable.
func TestInterceptJudgeUsageWithoutDatabase(t *testing.T) {
	s := &Server{m: &Manager{}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/intercept/judge/usage", nil)
	s.interceptJudgeUsage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var out db.JudgeUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("Response JSON parsing failed: %v", err)
	}
	if out.Calls != 0 || len(out.Daily) != 0 {
		t.Errorf("Without a database the series must be empty: %+v", out)
	}
}

package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Autumn-27/artex/db"
)

// containsHan detects Han code points independently of English, digits, punctuation and Hangul.
func containsHan(s string) (rune, bool) {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return r, true
		}
	}
	return 0, false
}

func sampleFindingNode(t *testing.T) *db.Node {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"vulnclass": "SQL Injection",
		"name":      "로그인 폼 SQL 주입",
		"severity":  "high",
		"summary":   "로그인 파라미터에서 오류 기반 SQL 주입이 확인되었습니다.",
		"evidence":  map[string]string{"poc": "' OR '1'='1"},
	})
	if err != nil {
		t.Fatalf("serialize payload: %v", err)
	}
	return &db.Node{ID: 1, Kind: "finding", Payload: payload}
}

func sampleDBFinding() *db.DBFinding {
	return &db.DBFinding{
		ID:              123,
		VulnClass:       "SQL Injection",
		Name:            "로그인 폼 SQL 주입",
		Severity:        "high",
		Summary:         "로그인 파라미터에서 오류 기반 SQL 주입이 확인되었습니다.",
		Evidence:        "' OR '1'='1",
		Status:          "confirmed",
		Report:          "상세 분석 본문.",
		CreatedAt:       time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC),
		EvidenceVersion: 2,
		TaskDescription: "데모 대상 침투 테스트",
		TrafficBindings: []db.FindingTrafficBinding{
			{
				ID:   9,
				Role: "request",
				Note: "주입 페이로드 전송 지점",
				Snapshot: db.TrafficEvidenceSnapshot{
					Method: "POST",
					URL:    "https://sandbox.local/login",
					Status: 200,
				},
			},
		},
	}
}

// Exploration Markdown report framing must be English with no Han labels.
func TestReportMarkdownLocalized(t *testing.T) {
	out := Markdown(Input{
		Title:       "데모 작업",
		Goal:        "샌드박스 전수 점검",
		GeneratedAt: time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC),
		AssetCounts: map[string]int{"host": 2, "url": 5},
		Findings:    []*db.Node{sampleFindingNode(t)},
	})
	if r, ok := containsHan(out); ok {
		t.Fatalf("report framing contains Han %q:\n%s", string(r), out)
	}
	for _, want := range []string{
		"# Security assessment report: 데모 작업",
		"- **Task objective**: 샌드박스 전수 점검",
		"- **Generated at**:",
		"## Summary",
		"- Confirmed findings: **1**",
		"## Findings",
		"**PoC / Evidence:**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report framing lacks %q", want)
		}
	}
}

// The empty-findings report guidance is English.
func TestReportMarkdownEmptyLocalized(t *testing.T) {
	out := Markdown(Input{Title: "빈 작업", GeneratedAt: time.Now()})
	if r, ok := containsHan(out); ok {
		t.Fatalf("empty report contains Han %q:\n%s", string(r), out)
	}
	if !strings.Contains(out, "_No vulnerabilities were confirmed in this assessment._") {
		t.Errorf("empty report guidance is not English:\n%s", out)
	}
}

// Combined and detailed findings exports use English framing and severity labels.
func TestFindingsMarkdownLocalized(t *testing.T) {
	f := sampleDBFinding()
	out := FindingsMarkdown([]*db.DBFinding{f}, time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC))
	if r, ok := containsHan(out); ok {
		t.Fatalf("finding summary contains Han %q:\n%s", string(r), out)
	}
	for _, want := range []string{
		"# Finding summary report",
		"- **Generated at**:",
		"- **Total findings**: 1",
		"## Summary",
		"| Severity | Count |",
		"| Critical |", "| High |", "| Medium |", "| Low |", // Severity terminology matches the UI.
		"## Finding details",
		"- **Type**: SQL Injection",
		"- **Status**: confirmed",
		"- **Task**: 데모 대상 침투 테스트",
		"- **Found at**:",
		"**Evidence:**",
		"**Detailed report:**",
		"## Related traffic evidence",
		"Evidence version: 2, bindings: 1.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary report lacks %q", want)
		}
	}

	single := SingleFindingMarkdown(f, time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC))
	if r, ok := containsHan(single); ok {
		t.Fatalf("single report contains Han %q:\n%s", string(r), single)
	}
	for _, want := range []string{
		"- **Severity**: high",
		"## Overview",
		"## Evidence",
		"## Detailed report",
		"[Request message](evidence/123/9/request.http)",
		"[Response message](evidence/123/9/response.http)",
	} {
		if !strings.Contains(single, want) {
			t.Errorf("single report lacks %q", want)
		}
	}
}

// CSV headers after the BOM are English; original data values remain unchanged.
func TestFindingsCSVLocalized(t *testing.T) {
	raw := FindingsCSV([]*db.DBFinding{sampleDBFinding()})
	out := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	header := strings.SplitN(out, "\n", 2)[0]
	if r, ok := containsHan(header); ok {
		t.Fatalf("CSV header contains Han %q: %s", string(r), header)
	}
	for _, col := range []string{"ID", "Name", "Type", "Severity", "Status", "Task", "Discovery time", "Overview", "Traffic evidence count", "Traffic evidence ID"} {
		if !strings.Contains(header, col) {
			t.Errorf("CSV header lacks column %q: %s", col, header)
		}
	}
}

import { expect, test } from "vitest";

import type { FindingRetest } from "@/lib/types";

import { assessmentReport } from "./report";

test("includes recorded retest outcomes without exporting source evidence or incomplete verdicts", () => {
  const retest: FindingRetest = {
    id: 12,
    finding_id: 41,
    conversation_id: null,
    status: "completed",
    verdict: "fixed",
    notes: "private retest notes",
    summary: "private retest summary",
    evidence: "private retest evidence",
    error: "",
    created_at: "2026-11-19T17:45:00Z",
    started_at: null,
    finished_at: "2026-11-19T18:00:00Z",
  };
  const report = assessmentReport(
    { title: "Portal assessment", scope: "Staging", exclusions: "Production", window: "" },
    "portal-task",
    "The staging assessment covered the portal.",
    "Production was outside scope.",
    [
      {
        finding: {
          finding_id: "41",
          id: "node",
          name: "Missing cookie flag",
          vulnclass: "Session management",
          severity: "medium",
          status: "fixed",
          summary: "private source summary",
          evidence: "private source evidence",
          ts: "2026-11-19T17:00:00Z",
        },
        observation: "Reviewed the cookie policy.",
        remediation: "Keep the flag enabled.",
        retests: [retest, { ...retest, id: 13, status: "stopped", verdict: "reproduced" }],
      },
    ],
  );

  expect(report).toContain("Finding #41");
  expect(report).toContain("Reviewed the cookie policy.");
  expect(report).toContain("#12 · 2026-11-19T18:00:00Z · completed · fixed");
  expect(report).toContain("#13 · 2026-11-19T18:00:00Z · stopped · No completed verdict");
  expect(report).not.toMatch(/private|reproduced/);
});

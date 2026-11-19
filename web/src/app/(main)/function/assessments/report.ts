import type { Finding, FindingRetest } from "@/lib/types";

export interface AssessmentPlan {
  title: string;
  scope: string;
  exclusions: string;
  window: string;
}

export interface ReviewedFinding {
  finding: Finding;
  observation: string;
  remediation: string;
  retests: FindingRetest[];
}

export function findingTitle(finding: Finding) {
  return finding.name?.trim() ? finding.name : finding.vulnclass || "Untitled finding";
}

export function assessmentReport(
  plan: AssessmentPlan,
  taskId: string,
  executive: string,
  limitations: string,
  reviews: ReviewedFinding[],
) {
  return [
    plan.title,
    `Source task: ${taskId}`,
    "",
    "EXECUTIVE SUMMARY",
    executive.trim() || "No executive summary supplied.",
    "",
    "SCOPE",
    plan.scope,
    "",
    "Exclusions",
    plan.exclusions || "None documented.",
    "",
    "Assessment window",
    plan.window || "Not documented.",
    "",
    "COVERAGE AND LIMITATIONS",
    limitations.trim() || "No coverage notes supplied.",
    "This report includes only findings selected for review. The documented scope does not establish test coverage.",
    "",
    "REVIEWED FINDINGS",
    reviews.length ? "" : "No findings were selected for this report.",
    ...reviews.flatMap(({ finding, observation, remediation, retests }) => [
      findingTitle(finding),
      `Finding #${finding.finding_id} · ${finding.severity} · ${finding.status.replaceAll("_", " ")}`,
      "",
      observation,
      "",
      "Recommended remediation",
      remediation,
      "",
      "Retest history (outcomes only)",
      ...(retests.length
        ? retests.map((retest) =>
            [
              `#${retest.id}`,
              retest.finished_at || retest.created_at,
              retest.status,
              retest.status === "completed" ? retest.verdict || "No verdict recorded" : "No completed verdict",
            ].join(" · "),
          )
        : ["No retests recorded."]),
      "",
    ]),
  ].join("\n");
}

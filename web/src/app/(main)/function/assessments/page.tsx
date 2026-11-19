"use client";

import { type FormEvent, useCallback, useEffect, useState } from "react";

import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { Finding, FindingRetest, FindingsPage, Task } from "@/lib/types";

import { type AssessmentPlan, assessmentReport, findingTitle, type ReviewedFinding } from "./report";

const emptyPlan: AssessmentPlan = { title: "", scope: "", exclusions: "", window: "" };
const pageSize = 20;
const steps = ["Scope", "Findings", "Report"] as const;
type Step = (typeof steps)[number];

type ReadResult<T> = { value: T } | { failed: true } | null;

function useRead<T>(read: () => Promise<T>) {
  const [state, setState] = useState<{ read: typeof read; result: ReadResult<T>; attempt: number } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    read().then(
      (value) => active && setState({ read, attempt, result: { value } }),
      () => active && setState({ read, attempt, result: { failed: true } }),
    );
    return () => {
      active = false;
    };
  }, [read, attempt]);

  return {
    result: state?.read === read && state.attempt === attempt ? state.result : null,
    retry: () => {
      setState(null);
      setAttempt((current) => current + 1);
    },
  };
}

function ReadError({ message, retry }: { message: string; retry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <p role="alert" className="text-destructive text-sm">
        {message}
      </p>
      <Button type="button" variant="outline" onClick={retry}>
        Retry
      </Button>
    </div>
  );
}

export default function AssessmentsPage() {
  const [step, setStep] = useState<Step>("Scope");
  const [plan, setPlan] = useState(emptyPlan);
  const [taskId, setTaskId] = useState("");
  const [reviews, setReviews] = useState<ReviewedFinding[]>([]);
  const [executive, setExecutive] = useState("");
  const [limitations, setLimitations] = useState("");
  const [downloaded, setDownloaded] = useState(false);
  const [scopeError, setScopeError] = useState("");
  const tasks = useRead(api.tasks);
  const task: Task | undefined =
    tasks.result && "value" in tasks.result ? tasks.result.value.tasks.find((item) => item.id === taskId) : undefined;

  function updatePlan(key: keyof AssessmentPlan, value: string) {
    setPlan((previous) => ({ ...previous, [key]: value }));
  }

  function continueToFindings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!plan.title.trim() || !plan.scope.trim()) {
      setScopeError("Enter an assessment name and at least one in-scope asset.");
      return;
    }
    if (!task) {
      setScopeError("Select an available source task.");
      return;
    }
    setScopeError("");
    setStep("Findings");
  }

  function saveReview(review: ReviewedFinding) {
    setReviews((previous) => [
      ...previous.filter((item) => item.finding.finding_id !== review.finding.finding_id),
      review,
    ]);
  }

  const report = assessmentReport(plan, taskId, executive, limitations, reviews);

  function downloadReport() {
    const url = URL.createObjectURL(new Blob([report], { type: "text/plain;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = "artex-assessment-report.txt";
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    setDownloaded(true);
  }

  return (
    <div className="mx-auto w-full min-w-0 max-w-5xl px-4 py-6 sm:px-8 sm:py-10">
      <header className="mb-6 space-y-2">
        <h1 className="font-semibold text-2xl tracking-tight sm:text-3xl">Assessment workspace</h1>
        <p className="text-muted-foreground text-sm">
          Document scope, review findings, and prepare a report from an existing task.
        </p>
        <p className="text-muted-foreground text-xs">
          Your draft is kept on this page until you leave or reload. Download the report to keep a copy.
        </p>
      </header>

      <nav aria-label="Assessment steps" className="mb-6">
        <ol className="flex flex-wrap gap-2">
          {steps.map((item, index) => (
            <li key={item}>
              <Button
                variant={step === item ? "default" : "outline"}
                aria-current={step === item ? "step" : undefined}
                disabled={index > steps.indexOf(step)}
                onClick={() => {
                  setStep(item);
                  setDownloaded(false);
                }}
              >
                {index + 1}. {item}
              </Button>
            </li>
          ))}
        </ol>
      </nav>

      <section className="min-w-0 space-y-5 rounded-xl border bg-card p-4 sm:p-6">
        {step === "Scope" && (
          <form onSubmit={continueToFindings} className="space-y-5">
            <div>
              <h2 className="font-semibold text-lg">Document the assessment</h2>
              <p className="text-muted-foreground text-sm">
                Use the agreed scope. This form records it for review and reporting.
              </p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="assessment-title">Assessment name</Label>
                <Input
                  id="assessment-title"
                  required
                  maxLength={120}
                  value={plan.title}
                  onChange={(e) => updatePlan("title", e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="assessment-window">Assessment window (optional)</Label>
                <Input
                  id="assessment-window"
                  maxLength={160}
                  placeholder="Dates, times, and time zone"
                  value={plan.window}
                  onChange={(e) => updatePlan("window", e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="assessment-scope">In-scope assets</Label>
                <Textarea
                  id="assessment-scope"
                  required
                  maxLength={4000}
                  placeholder="List agreed applications or environments"
                  value={plan.scope}
                  onChange={(e) => updatePlan("scope", e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="assessment-exclusions">Exclusions (optional)</Label>
                <Textarea
                  id="assessment-exclusions"
                  maxLength={4000}
                  placeholder="List assets or activities outside the agreement"
                  value={plan.exclusions}
                  onChange={(e) => updatePlan("exclusions", e.target.value)}
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="assessment-task">Source task</Label>
              {tasks.result && "failed" in tasks.result ? (
                <ReadError message="Could not load tasks." retry={tasks.retry} />
              ) : (
                <select
                  id="assessment-task"
                  className="h-10 w-full rounded-md border bg-background px-3 text-sm focus-visible:outline-2"
                  required
                  disabled={!tasks.result}
                  value={taskId}
                  onChange={(e) => {
                    setTaskId(e.target.value);
                    setReviews([]);
                  }}
                >
                  <option value="">{tasks.result ? "Choose a task" : "Loading tasks…"}</option>
                  {tasks.result?.value.tasks.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name?.trim() ? item.name : item.description || item.id} ({item.status})
                    </option>
                  ))}
                </select>
              )}
              {tasks.result && "value" in tasks.result && tasks.result.value.tasks.length === 0 && (
                <p className="text-muted-foreground text-sm">
                  No source tasks are available.{" "}
                  <Link className="underline" href="/function/tasks">
                    Open Tasks
                  </Link>
                </p>
              )}
              {reviews.length > 0 && (
                <p className="text-muted-foreground text-sm">
                  Changing the source task clears the finding reviews in this draft.
                </p>
              )}
            </div>
            {scopeError && (
              <p role="alert" className="text-destructive text-sm">
                {scopeError}
              </p>
            )}
            <Button type="submit" disabled={!task}>
              Continue to findings
            </Button>
          </form>
        )}

        {step === "Findings" && task && (
          <FindingWorkspace
            task={task}
            reviews={reviews}
            saveReview={saveReview}
            removeReview={(id) => setReviews((previous) => previous.filter((item) => item.finding.finding_id !== id))}
            openReport={() => setStep("Report")}
          />
        )}

        {step === "Report" && (
          <>
            <div>
              <h2 className="font-semibold text-lg">Prepare the report</h2>
              <p className="text-muted-foreground text-sm">
                Review the preview before sharing. It includes your written notes and recorded retest outcomes.
              </p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="assessment-summary">Executive summary</Label>
                <Textarea
                  id="assessment-summary"
                  maxLength={5000}
                  value={executive}
                  onChange={(e) => {
                    setExecutive(e.target.value);
                    setDownloaded(false);
                  }}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="assessment-limitations">Coverage and limitations</Label>
                <Textarea
                  id="assessment-limitations"
                  maxLength={5000}
                  value={limitations}
                  onChange={(e) => {
                    setLimitations(e.target.value);
                    setDownloaded(false);
                  }}
                />
              </div>
            </div>
            <h3 className="font-medium text-sm">Report preview</h3>
            <pre className="whitespace-pre-wrap break-words rounded-lg border bg-muted/30 p-4 text-sm">{report}</pre>
            <Button onClick={downloadReport}>Download report (.txt)</Button>
            {downloaded && (
              <p role="status" className="text-sm">
                Report download started.
              </p>
            )}
          </>
        )}
      </section>
    </div>
  );
}

function FindingWorkspace({
  task,
  reviews,
  saveReview,
  removeReview,
  openReport,
}: {
  task: Task;
  reviews: ReviewedFinding[];
  saveReview: (review: ReviewedFinding) => void;
  removeReview: (id: string) => void;
  openReport: () => void;
}) {
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Finding | null>(null);
  const read = useCallback(
    () => api.findingsPage({ task: task.id, page, pageSize, sort: "severity" }),
    [task.id, page],
  );
  const findings = useRead<FindingsPage>(read);
  const data = findings.result && "value" in findings.result ? findings.result.value : null;

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-semibold text-lg">Review findings</h2>
          <p className="break-words text-muted-foreground text-sm">
            {task.name?.trim() ? task.name : task.description || task.id} · {task.status}
          </p>
        </div>
        <Button onClick={openReport}>Continue to report</Button>
      </div>
      <p aria-live="polite" className="text-sm">
        {reviews.length} selected for the report
      </p>
      {reviews.length > 0 && (
        <details className="rounded-lg border p-3 text-sm">
          <summary className="cursor-pointer">Manage report selections</summary>
          <ul className="mt-2 divide-y">
            {reviews.map(({ finding }) => (
              <li key={finding.finding_id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                <span className="min-w-0 break-words">{findingTitle(finding)}</span>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => removeReview(finding.finding_id || "")}
                >
                  Remove from report
                </Button>
              </li>
            ))}
          </ul>
        </details>
      )}

      {findings.result && "failed" in findings.result && (
        <ReadError message="Could not load findings for this task." retry={findings.retry} />
      )}
      {!findings.result && <p role="status">Loading findings…</p>}
      {data && (
        <>
          {data.items.length === 0 && (
            <p className="text-muted-foreground text-sm">
              No findings on this page. You can still report scope and coverage.
            </p>
          )}
          <ul className="divide-y rounded-lg border">
            {data.items.map((finding) => (
              <li key={finding.finding_id || finding.id} className="p-3 sm:p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 space-y-2">
                    <h3 className="break-words font-medium">{findingTitle(finding)}</h3>
                    <div className="flex flex-wrap gap-2">
                      <Badge variant="outline">{finding.severity}</Badge>
                      <Badge variant="secondary">{finding.status.replaceAll("_", " ")}</Badge>
                      <span className="text-muted-foreground text-xs">
                        {finding.traffic_count ?? 0} linked traffic records
                      </span>
                      {finding.report_stale && <Badge variant="outline">Evidence changed since report</Badge>}
                    </div>
                  </div>
                  {finding.finding_id && (
                    <Button
                      variant="outline"
                      size="sm"
                      aria-expanded={selected?.finding_id === finding.finding_id}
                      aria-label={`Review ${findingTitle(finding)}`}
                      onClick={() =>
                        setSelected((current) => (current?.finding_id === finding.finding_id ? null : finding))
                      }
                    >
                      {selected?.finding_id === finding.finding_id ? "Hide review" : "Review"}
                    </Button>
                  )}
                </div>
                {finding.finding_id && selected?.finding_id === finding.finding_id && (
                  <FindingReview
                    key={finding.finding_id}
                    id={finding.finding_id}
                    finding={finding}
                    previous={reviews.find((item) => item.finding.finding_id === finding.finding_id)}
                    onSave={saveReview}
                  />
                )}
              </li>
            ))}
          </ul>
          {(page > 1 || data.total > pageSize) && (
            <nav aria-label="Finding pages" className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                disabled={page === 1}
                onClick={() => {
                  setSelected(null);
                  setPage(page - 1);
                }}
              >
                Previous
              </Button>
              <p className="text-sm">
                Page {page} · {data.total} findings
              </p>
              <Button
                variant="outline"
                disabled={page * pageSize >= data.total}
                onClick={() => {
                  setSelected(null);
                  setPage(page + 1);
                }}
              >
                Next
              </Button>
            </nav>
          )}
        </>
      )}
    </>
  );
}

function FindingReview({
  id,
  finding,
  previous,
  onSave,
}: {
  id: string;
  finding: Finding;
  previous?: ReviewedFinding;
  onSave: (review: ReviewedFinding) => void;
}) {
  const read = useCallback(() => api.findingRetests(id), [id]);
  const history = useRead<FindingRetest[]>(read);
  const [observation, setObservation] = useState(previous?.observation ?? "");
  const [remediation, setRemediation] = useState(previous?.remediation ?? "");
  const [saved, setSaved] = useState(false);
  const retests = history.result && "value" in history.result ? history.result.value : null;
  const valid = observation.trim() && remediation.trim();

  return (
    <form
      className="mt-4 space-y-4 rounded-lg bg-muted/30 p-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!retests || !valid) return;
        onSave({ finding, observation: observation.trim(), remediation: remediation.trim(), retests });
        setSaved(true);
      }}
    >
      <p className="whitespace-pre-wrap break-words text-sm">{finding.summary || "No source summary available."}</p>
      <Link
        className="inline-block text-sm underline underline-offset-4"
        href={`/function/findings/detail?id=${encodeURIComponent(id)}`}
        target="_blank"
        rel="noreferrer"
      >
        Open source finding and evidence (new tab)
      </Link>
      {history.result && "failed" in history.result && (
        <ReadError message="Could not load retest history. Retry before adding this finding." retry={history.retry} />
      )}
      {!history.result && (
        <p role="status" className="text-sm">
          Loading retest history…
        </p>
      )}
      {retests && (
        <div className="space-y-1 text-sm">
          <h4 className="font-medium">Retest outcomes</h4>
          {retests.length ? (
            <ul className="space-y-1">
              {retests.map((item) => (
                <li key={item.id}>
                  #{item.id} · {item.status}
                  {item.status === "completed" && item.verdict ? ` · ${item.verdict}` : ""}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground">No retests recorded.</p>
          )}
        </div>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor={`observation-${id}`}>Reviewed observation and impact</Label>
          <Textarea
            id={`observation-${id}`}
            required
            maxLength={5000}
            value={observation}
            onChange={(event) => {
              setObservation(event.target.value);
              setSaved(false);
            }}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor={`remediation-${id}`}>Recommended remediation</Label>
          <Textarea
            id={`remediation-${id}`}
            required
            maxLength={5000}
            value={remediation}
            onChange={(event) => {
              setRemediation(event.target.value);
              setSaved(false);
            }}
          />
        </div>
      </div>
      <Button type="submit" disabled={!retests || !valid}>
        {previous ? "Update report selection" : "Add to report"}
      </Button>
      {saved && (
        <p role="status" className="text-sm">
          Finding saved to report draft.
        </p>
      )}
    </form>
  );
}

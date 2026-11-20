"use client";

import * as React from "react";

import Link from "next/link";

import { CheckCircle2Icon, CircleAlertIcon, CopyIcon, GlobeIcon, RefreshCwIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { Activity, Task } from "@/lib/types";
import { copyText } from "@/lib/utils";
import { shellQuote, targetFromDescription, websiteTarget } from "@/lib/website-assessment";

type Readiness = Awaited<ReturnType<typeof api.setupReadiness>>;

export function AssessmentSetup({ taskId, onOpenSessions }: { taskId: string; onOpenSessions: () => void }) {
  const [task, setTask] = React.useState<Task | null>(null);
  const [readiness, setReadiness] = React.useState<Readiness | null>(null);
  const [events, setEvents] = React.useState<Activity[]>([]);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");
  const [target, setTarget] = React.useState("");
  const [retryGoal, setRetryGoal] = React.useState<string | null>(null);
  const [copied, setCopied] = React.useState(false);
  const mounted = React.useRef(true);
  const load = React.useCallback(
    async (visible = true) => {
      if (visible) setBusy(true);
      setError("");
      try {
        const [current, setup, activity] = await Promise.all([
          api.task(taskId),
          api.setupReadiness(taskId),
          api.activity(taskId, { limit: 100 }),
        ]);
        if (!mounted.current) return;
        setTask(current);
        setReadiness(setup);
        setEvents(activity.items);
        setTarget((value) => value || targetFromDescription(current.description));
      } catch {
        if (mounted.current) {
          setReadiness(null);
          setError("Could not check setup. Try again; your assessment has not been changed.");
        }
      } finally {
        if (mounted.current && visible) setBusy(false);
      }
    },
    [taskId],
  );
  React.useEffect(() => {
    mounted.current = true;
    void load();
    return () => {
      mounted.current = false;
    };
  }, [load]);
  const terminal = task && ["done", "failed", "timeout"].includes(task.status);
  React.useEffect(() => {
    if (!task || task.paused || terminal) return;
    const timer = setInterval(() => {
      void load(false);
    }, 10000);
    return () => clearInterval(timer);
  }, [task, terminal, load]);
  const latestError = events.filter((event) => event.is_error && !event.inherited).at(-1);
  const ready = readiness?.worker === "connected" && readiness.target_approved;
  const stateTitle = !task
    ? "Checking assessment setup"
    : terminal
      ? "Run ended — review the evidence"
      : task.paused
        ? "Saved and paused"
        : task.queued
          ? "Waiting to run"
          : "Assessment in progress";
  async function start() {
    setBusy(true);
    setError("");
    try {
      await api.controlTask(taskId, "resume");
      setNotice("Assessment started. Open execution details to follow its progress.");
      await load();
    } catch {
      setError("Could not start the assessment. Check setup and try again.");
    } finally {
      setBusy(false);
    }
  }
  async function copyApproval() {
    try {
      const url = websiteTarget(target);
      const command = `python3 scripts/approve-website.py --task ${shellQuote(taskId)} --url ${shellQuote(url.href)}`;
      if (!(await copyText(command))) throw new Error("Copy failed. Select and copy the command manually.");
      setCopied(true);
      setNotice("Command copied. Run it from your ARTEX installation folder in Terminal.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not copy the setup command.");
    }
  }
  async function restartWithGoal() {
    if (!retryGoal?.trim()) return;
    setBusy(true);
    setError("");
    try {
      await api.addGoal(taskId, retryGoal.trim());
      setRetryGoal(null);
      setNotice("Follow-up added. Planning has resumed; provider usage may incur charges.");
      await load();
    } catch {
      setError("Could not add the follow-up. Try again before creating another one.");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card className="border-primary/25" aria-busy={busy}>
      <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <p className="font-medium text-primary text-sm">Assessment #{taskId}</p>
          <CardTitle className="flex items-center gap-2 text-xl">
            <GlobeIcon className="size-5" />
            {stateTitle}
          </CardTitle>
          <p className="text-muted-foreground text-sm leading-relaxed">
            Finish setup before starting. To see what was actually tested, open execution details.
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()} disabled={busy}>
          <RefreshCwIcon className={busy ? "animate-spin" : ""} />
          Check setup
        </Button>
      </CardHeader>
      <CardContent className="grid gap-5">
        {readiness && (
          <div className="grid gap-3 sm:grid-cols-2">
            <SetupStep
              good={readiness.worker === "connected"}
              title="Scanning service"
              detail={
                readiness.worker === "connected"
                  ? "Connected. Run a check to verify that it can reach your website."
                  : readiness.worker === "not_configured"
                    ? "Scanning is not enabled in this installation yet."
                    : "Cannot connect. The scanning service may be busy, stopped, or need an update."
              }
            />
            <SetupStep
              good={readiness.target_approved}
              title="Target permission"
              detail={
                readiness.target_approved
                  ? "Approved server addresses are configured. Approve again if your server’s address changes."
                  : "Approve this website’s server address before testing can start."
              }
            />
          </div>
        )}
        {readiness && !ready && (
          <details className="rounded-lg border bg-muted/20 p-4 text-sm">
            <summary className="cursor-pointer font-medium">How to finish setup</summary>
            <div className="mt-4 grid gap-3">
              <p className="text-muted-foreground leading-relaxed">
                Target approval stays outside the app so assessment agents cannot change their own network permissions.
                This one-time helper resolves the website, shows the exact server addresses for your approval, and
                restarts only this installation’s scanning service. It does not start testing.
              </p>
              <Label htmlFor={`approval-url-${taskId}`}>Website to approve</Label>
              <Input
                id={`approval-url-${taskId}`}
                value={target}
                placeholder="https://example.com"
                onChange={(e) => {
                  setTarget(e.target.value);
                  setCopied(false);
                }}
              />
              <Button variant="outline" onClick={() => void copyApproval()}>
                <CopyIcon />
                {copied ? "Copy setup command again" : "Copy target approval command"}
              </Button>
              <p className="text-muted-foreground">
                Run it in Terminal from your installation folder, using the same Docker context as ARTEX. Review the
                addresses and type APPROVE only for your server. Then choose Check setup above. The helper requires the
                scanning service to be running.
              </p>
              <Link href="/system/llm" className="underline underline-offset-4">
                Manage AI providers
              </Link>
            </div>
          </details>
        )}
        {latestError && (
          <div className="rounded-lg border border-amber-500/40 p-4 text-sm">
            <p className="mb-1 font-medium">A recent operation failed</p>
            <p className="break-words text-muted-foreground">{latestError.summary.slice(0, 400)}</p>
            <p className="mt-2 text-muted-foreground">
              This may be from an earlier attempt. Open execution details to check its time and full context.
            </p>
          </div>
        )}
        {terminal && (
          <div className="rounded-lg border bg-muted/20 p-4 text-sm leading-relaxed">
            <p className="font-medium">Zero findings does not mean the site passed.</p>
            <p className="text-muted-foreground">
              Check that execution details contain real target responses. Asset records, planning, and a completed
              status alone are not evidence of successful testing.
            </p>
          </div>
        )}
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        {notice && (
          <p role="status" className="text-sm">
            {notice}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {task?.paused && !terminal && (
            <Button disabled={!ready || busy} onClick={() => void start()}>
              Start assessment
            </Button>
          )}
          <Button variant="outline" onClick={onOpenSessions}>
            View execution details
          </Button>
          {terminal && ready && targetFromDescription(task.description) && (
            <Button
              variant="outline"
              disabled={busy || retryGoal !== null}
              onClick={() =>
                setRetryGoal(
                  `Repeat the homepage baseline for ${targetFromDescription(task.description)} now that setup has been checked. Make one certificate-validated GET, without retries, redirects, or subresources. Record TLS, status, security headers, and cookie attributes with values redacted. Preserve every existing scope restriction. Earlier tool failures do not count as completed testing. If blocked, report the exact error and mark the check incomplete.`,
                )
              }
            >
              Prepare a homepage recheck
            </Button>
          )}
        </div>
        {task?.paused && !terminal && !ready && (
          <p className="text-muted-foreground text-sm">
            Start becomes available after both setup checks pass. Provider availability is checked when the task is
            admitted.
          </p>
        )}
        {retryGoal !== null && (
          <div className="grid gap-3 rounded-lg border p-4">
            <Label htmlFor="recheck-goal">Review the follow-up</Label>
            <Textarea
              id="recheck-goal"
              rows={6}
              value={retryGoal}
              onChange={(e) => setRetryGoal(e.target.value)}
              disabled={busy}
            />
            <p className="text-muted-foreground text-sm">
              Adding this follow-up restarts planning and may incur provider charges.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button disabled={busy || !retryGoal.trim()} onClick={() => void restartWithGoal()}>
                Add follow-up and restart
              </Button>
              <Button variant="outline" disabled={busy} onClick={() => setRetryGoal(null)}>
                Cancel
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function SetupStep({ good, title, detail }: { good: boolean; title: string; detail: string }) {
  const Icon = good ? CheckCircle2Icon : CircleAlertIcon;
  return (
    <div className="flex gap-3 rounded-lg border p-4">
      <Icon
        className={`mt-0.5 size-5 shrink-0 ${good ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"}`}
      />
      <div>
        <p className="font-medium text-sm">{title}</p>
        <p className="mt-1 text-muted-foreground text-sm leading-relaxed">{detail}</p>
      </div>
    </div>
  );
}

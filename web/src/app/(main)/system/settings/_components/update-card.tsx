"use client";

import * as React from "react";

import {
  CheckCircle2Icon,
  CopyIcon,
  DownloadIcon,
  ExternalLinkIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { toast } from "sonner";
import { useTranslations } from "use-intl";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { api, sseUrl } from "@/lib/api";
import type { UpdateCheck, UpdateProgress } from "@/lib/types";

/* Maximum wait for a deliberately enabled staged update. Three startup phases are normally seconds; three minutes accommodates slow disks or container recreation. */
const RESTART_TIMEOUT_MS = 180_000;

function humanSize(n?: number): string {
  if (!n || n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export function UpdateCard() {
  const t = useTranslations("settingsPage");
  const richTags = {
    code: (chunks: React.ReactNode) => <span className="font-mono">{chunks}</span>,
  };
  const [info, setInfo] = React.useState<UpdateCheck | null>(null);
  const [checking, setChecking] = React.useState(true);
  const [progress, setProgress] = React.useState<UpdateProgress | null>(null);
  // Track restart separately from download progress because staging ends the process and SSE connection.
  const [restarting, setRestarting] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  // Unknown or absent capabilities fail closed, including older server responses.
  const inProcessUpdates = info?.in_process_updates === true;

  // Automatic entry checks keep the backend cache; explicit user checks
  // force a fresh result so recently published versions need not wait for cache expiry.
  const check = React.useCallback(
    (quiet = false) => {
      setChecking(true);
      api
        .checkUpdate(!quiet)
        .then((r) => {
          setInfo(r);
          if (!quiet) {
            if (r.error) toast.error(t("update.toast.checkFailed", { error: r.error }));
            else if (r.has_update) toast.success(t("update.toast.found", { latest: r.latest ?? "" }));
            else if (r.comparable) toast.success(t("update.toast.upToDate"));
          }
        })
        .catch((e) => {
          if (!quiet) toast.error(t("update.toast.checkFailed", { error: (e as Error).message }));
        })
        .finally(() => setChecking(false));
    },
    [t],
  );

  React.useEffect(() => {
    check(true);
  }, [check]);

  // Poll health until the version actually changes.
  //
  // Reachability alone is insufficient: the old binary can briefly restart only
  // to replace artex.new and exit again.
  const waitForNewVersion = React.useCallback(
    async (fromVersion: string) => {
      setRestarting(true);
      const deadline = Date.now() + RESTART_TIMEOUT_MS;
      while (Date.now() < deadline) {
        await sleep(2000);
        try {
          const r = await fetch("/api/health", { cache: "no-store" });
          if (r.ok) {
            const j = (await r.json()) as { version?: string };
            if (j.version && j.version !== fromVersion) {
              toast.success(t("update.toast.updated", { version: j.version }));
              await sleep(800);
              window.location.reload();
              return;
            }
          }
        } catch {
          // Temporary unreachability during restart is expected; continue polling.
        }
      }
      setRestarting(false);
      toast.error(t("update.toast.restartTimeout"));
    },
    [t],
  );

  // Subscribe through the authenticated same-origin update stream.
  const openStream = React.useCallback(
    (fromVersion: string) => {
      const es = new EventSource(sseUrl("/api/update/stream"));
      es.onmessage = (ev) => {
        let p: UpdateProgress;
        try {
          p = JSON.parse(ev.data) as UpdateProgress;
        } catch {
          return;
        }
        setProgress(p);
        if (p.phase === "failed") {
          es.close();
          setBusy(false);
          toast.error(t("update.toast.updateFailed", { error: p.error || p.message }));
          return;
        }
        if (p.phase === "staged") {
          es.close();
          void waitForNewVersion(fromVersion);
        }
      };
      es.onerror = () => {
        // SSE necessarily ends when the process exits; once restart waiting begins,
        // health-version polling owns completion detection.
        es.close();
      };
      return es;
    },
    [waitForNewVersion, t],
  );

  const doUpdate = () => {
    if (info?.in_process_updates !== true) return;
    const from = info.current;
    const ok = window.confirm(
      `${t("update.confirm.title", { latest: info.latest ?? "" })}\n\n` +
        t("update.confirm.restart") +
        (info.mode === "docker" ? `\n\n${t("update.confirm.docker")}` : ""),
    );
    if (!ok) return;

    setBusy(true);
    setProgress({ phase: "downloading", percent: 0, message: t("update.toast.preparing") });
    const es = openStream(from);
    api.applyUpdate().catch((e) => {
      es.close();
      setBusy(false);
      setProgress(null);
      toast.error(t("update.toast.startFailed", { error: (e as Error).message }));
    });
  };

  const doRollback = () => {
    if (info?.in_process_updates !== true) return;
    if (!window.confirm(t("update.confirm.rollback"))) return;
    const from = info.current;
    setBusy(true);
    api
      .rollbackUpdate()
      .then(() => {
        toast.success(t("update.toast.rollbackSwitched"));
        void waitForNewVersion(from);
      })
      .catch((e) => {
        setBusy(false);
        toast.error(t("update.toast.rollbackFailed", { error: (e as Error).message }));
      });
  };

  const copyVersion = async () => {
    if (!info?.current) return;
    try {
      await navigator.clipboard.writeText(info.current);
      toast.success(t("update.toast.versionCopied"));
    } catch {
      toast.error(t("update.toast.copyFailed"));
    }
  };

  const phase = progress?.phase;
  const showProgress = busy || restarting;
  // Only download has a measured Content-Length percentage; verification/extraction/restart
  // use indeterminate pulse progress rather than inventing a remaining duration.
  const downloading = !restarting && phase === "downloading";
  const pct = downloading ? Math.max(progress?.percent ?? 0, 0) : 100;

  return (
    // This card owns vertical margins and avoids splitting across the settings columns.
    <Card className="mb-4 break-inside-avoid md:mb-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <DownloadIcon className="size-4" />
          {t("update.title")}
        </CardTitle>
        <CardDescription>{t("update.desc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t("update.currentVersion")}</span>
          <Badge variant="secondary" className="font-mono">
            {info?.current ?? "…"}
          </Badge>
          {info && (
            <>
              <Badge variant="outline" className="font-mono">
                {info.os}/{info.arch}
              </Badge>
              <Badge variant="outline">
                {info.mode === "docker" ? t("update.modeDocker") : t("update.modeStandalone")}
              </Badge>
            </>
          )}
          {info?.latest && (
            <>
              <span className="text-muted-foreground">{t("update.latestVersion")}</span>
              <Badge variant={info.has_update ? "default" : "secondary"} className="font-mono">
                {info.latest}
              </Badge>
            </>
          )}
          {info?.html_url && (
            <a
              href={info.html_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
            >
              {t("update.changelog")} <ExternalLinkIcon className="size-3" />
            </a>
          )}
        </div>

        {!inProcessUpdates && (
          <div className="space-y-2 rounded-md border p-3 text-sm">
            <p className="font-medium">{t("update.externalOnlyTitle")}</p>
            <p className="text-muted-foreground">{t("update.externalOnlyBody")}</p>
            {info?.installation_instructions && (
              <p className="text-xs text-muted-foreground">{info.installation_instructions}</p>
            )}
            <a
              href="https://github.com/jaysonguglietta/artexV2/blob/main/docs/SIGNED-RELEASES.md"
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 underline underline-offset-4"
            >
              {t("update.verifiedInstallGuide")} <ExternalLinkIcon className="size-3" />
            </a>
          </div>
        )}

        {info?.boot_notice && (
          <p className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {info.boot_notice}
          </p>
        )}

        {info?.error && (
          <p className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {t("update.connectError", { error: info.error })}
          </p>
        )}

        {info && !info.comparable && info.reason && <p className="text-xs text-muted-foreground">{info.reason}</p>}

        {info?.has_update && info.asset_available === false && (
          <p className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {t("update.noAsset", { latest: info.latest ?? "", os: info.os, arch: info.arch, asset: info.asset ?? "" })}
          </p>
        )}

        {inProcessUpdates && info?.has_update && info.asset_available !== false && (
          <p className="text-xs text-muted-foreground">
            {t("update.willDownloadPrefix")}
            <span className="font-mono">{info.asset}</span>
            {info.size ? t("update.willDownloadSize", { size: humanSize(info.size) }) : ""}
            {t("update.willDownloadSuffix")}
          </p>
        )}

        {info && !info.has_update && info.comparable && !info.error && (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <CheckCircle2Icon className="size-3.5 text-emerald-600" />
            {t("update.upToDate")}
          </p>
        )}

        {inProcessUpdates && info?.mode === "docker" && info.has_update && (
          <p className="text-xs text-muted-foreground">{t.rich("update.dockerNote", richTags)}</p>
        )}

        {showProgress && (
          <div className="space-y-1.5">
            <Progress value={pct} className={downloading ? undefined : "animate-pulse"} />
            <p className="text-xs text-muted-foreground">{restarting ? t("update.restarting") : progress?.message}</p>
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => check(false)} disabled={checking || busy || restarting}>
            <RefreshCwIcon className={checking ? "size-4 animate-spin" : "size-4"} />
            {t("update.check")}
          </Button>
          {info?.current && (
            <Button variant="ghost" size="sm" onClick={copyVersion}>
              <CopyIcon className="size-4" />
              {t("update.copyVersion")}
            </Button>
          )}
          {inProcessUpdates && (
            <Button
              size="sm"
              onClick={doUpdate}
              disabled={busy || restarting || !info?.has_update || info?.asset_available === false}
            >
              <DownloadIcon className="size-4" />
              {info?.has_update ? t("update.updateToVersion", { latest: info.latest ?? "" }) : t("update.updateNow")}
            </Button>
          )}
          {inProcessUpdates && info?.has_backup && (
            <Button variant="ghost" size="sm" onClick={doRollback} disabled={busy || restarting}>
              <RotateCcwIcon className="size-4" />
              {t("update.rollback")}
            </Button>
          )}
        </div>

        <p className="text-xs text-muted-foreground">{t.rich("update.footer", richTags)}</p>
      </CardContent>
    </Card>
  );
}

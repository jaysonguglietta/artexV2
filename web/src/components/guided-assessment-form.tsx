"use client";

import * as React from "react";

import Link from "next/link";

import { GlobeIcon, ShieldCheckIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { LLMProfile } from "@/lib/types";
import { limitedWebsiteAssessment } from "@/lib/website-assessment";

export function GuidedAssessmentForm({
  profiles,
  loading,
  onSave,
  busy,
}: {
  profiles: LLMProfile[];
  loading: boolean;
  busy: boolean;
  onSave: (assessment: ReturnType<typeof limitedWebsiteAssessment>, profileID: number) => Promise<void>;
}) {
  const [website, setWebsite] = React.useState("");
  const [profile, setProfile] = React.useState("");
  const [authorized, setAuthorized] = React.useState(false);
  const [error, setError] = React.useState("");
  const selected = profile || profiles.find((p) => p.is_default)?.id || profiles[0]?.id || "";
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const assessment = limitedWebsiteAssessment(website);
      if (!authorized) throw new Error("Confirm that you own this website or have permission to assess it.");
      if (!selected) throw new Error("Add an AI provider before saving your assessment.");
      await onSave(assessment, Number(selected));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save. Please try again.");
    }
  }
  return (
    <form onSubmit={submit} className="grid gap-6" aria-busy={busy}>
      <div className="rounded-xl border bg-muted/30 p-5">
        <div className="mb-2 flex items-center gap-2 font-medium">
          <ShieldCheckIcon className="size-5 text-primary" /> A gentle first look at your website
        </div>
        <p className="text-muted-foreground text-sm leading-relaxed">
          Check HTTPS, security headers, cookie settings, and a few public pages. No password guessing, form
          submissions, exploitation, or broad scanning.
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="guided-website">Which website would you like to check?</Label>
        <div className="relative">
          <GlobeIcon className="absolute top-3 left-3 size-4 text-muted-foreground" aria-hidden="true" />
          <Input
            id="guided-website"
            autoComplete="url"
            placeholder="https://example.com"
            className="pl-9"
            value={website}
            onChange={(e) => {
              setWebsite(e.target.value);
              setError("");
            }}
            disabled={busy}
            required
            aria-describedby="website-help guided-error"
          />
        </div>
        <p id="website-help" className="text-muted-foreground text-sm">
          You can enter a domain name. We’ll use HTTPS. Only this host is included.
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="guided-model">AI provider</Label>
        {loading ? (
          <p role="status" className="text-muted-foreground text-sm">
            Loading your providers…
          </p>
        ) : profiles.length ? (
          <select
            id="guided-model"
            className="h-10 w-full rounded-md border bg-background px-3 text-sm"
            value={selected}
            onChange={(e) => setProfile(e.target.value)}
            disabled={busy}
          >
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} · {p.model}
              </option>
            ))}
          </select>
        ) : (
          <div className="rounded-md border border-amber-500/40 p-3 text-sm">
            Add a provider and API key first.{" "}
            <Link className="underline underline-offset-4" href="/system/llm">
              Open AI provider settings
            </Link>
            , then return and reopen this form.
          </div>
        )}
        <p className="text-muted-foreground text-sm">
          Your provider may charge for model usage. Saving this setup does not start it.
        </p>
      </div>
      <label
        htmlFor="guided-authorization"
        className="flex items-start gap-3 rounded-lg border p-4 text-sm leading-relaxed"
      >
        <Checkbox
          id="guided-authorization"
          className="mt-1"
          checked={authorized}
          onCheckedChange={(value) => setAuthorized(value === true)}
          disabled={busy}
        />
        <span>I own this website or have permission to assess it.</span>
      </label>
      <div className="space-y-2 text-sm">
        <p className="font-medium">What happens next?</p>
        <p className="text-muted-foreground leading-relaxed">
          We save a paused assessment with a 15-minute time budget. Its setup checklist explains target approval. You
          choose when to start.
        </p>
      </div>
      <p id="guided-error" role={error ? "alert" : undefined} className="text-destructive text-sm">
        {error}
      </p>
      <Button
        type="submit"
        className="w-full"
        disabled={busy || loading || !profiles.length || !authorized || !website.trim()}
      >
        {busy ? "Saving assessment…" : "Save and review setup"}
      </Button>
    </form>
  );
}

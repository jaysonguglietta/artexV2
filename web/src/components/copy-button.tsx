"use client";

import * as React from "react";

import { CheckIcon, CopyIcon } from "lucide-react";
import { toast } from "sonner";
import { useTranslations } from "use-intl";

import { Button } from "@/components/ui/button";
import { cn, copyText } from "@/lib/utils";

type CopyButtonProps = {
  // Text to copy; empty disables the control.
  text: string | null | undefined;
  // Success toast text, defaulting to the copied message.
  successMessage?: string;
  label?: React.ReactNode;
  size?: React.ComponentProps<typeof Button>["size"];
  variant?: React.ComponentProps<typeof Button>["variant"];
  className?: string;
};

// Shared clipboard button with success/failure feedback and an insecure-HTTP
// fallback provided by copyText.
export function CopyButton({
  text,
  successMessage,
  label,
  size = "sm",
  variant = "outline",
  className,
}: CopyButtonProps) {
  const t = useTranslations("copyButton");
  const [copied, setCopied] = React.useState(false);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  async function handleCopy() {
    if (!text) return;
    const ok = await copyText(text);
    if (ok) {
      setCopied(true);
      toast.success(successMessage ?? t("success"));
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t("error"));
    }
  }

  return (
    <Button type="button" size={size} variant={variant} className={cn(className)} disabled={!text} onClick={handleCopy}>
      {copied ? <CheckIcon /> : <CopyIcon />}
      {label ?? t("label")}
    </Button>
  );
}

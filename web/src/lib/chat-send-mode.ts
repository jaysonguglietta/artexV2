"use client";

import * as React from "react";

import { useTranslations } from "use-intl";

import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

// Send/newline keyboard preference is browser-only localStorage state,
// not server/account state; another browser needs its own preference.
// Issue #39 restored the earlier Ctrl+Enter option after version 0.3.2
// changed the default to Enter-to-send.
export type ChatSendMode = "enter" | "ctrl-enter";

export const CHAT_SEND_MODE_KEY = "artex_chat_send_mode";
export const DEFAULT_CHAT_SEND_MODE: ChatSendMode = "enter";

// The settings selector obtains its display labels from the English message hook,
// rather than hard-coding presentation text in this pure module.
// Stable values remain storage and behavior identifiers.
export function useChatSendModeOptions(): { value: ChatSendMode; label: string }[] {
  const t = useTranslations("chatSendMode.option");
  return [
    { value: "enter", label: t("enter") },
    { value: "ctrl-enter", label: t("ctrlEnter") },
  ];
}

function parseMode(raw: string | null): ChatSendMode {
  return raw === "ctrl-enter" || raw === "enter" ? raw : DEFAULT_CHAT_SEND_MODE;
}

// Subscribers within this tab need an explicit notification because storage events
// fire only in other tabs. Emit after local changes so open inputs update
// without requiring a refresh.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

// String snapshots compare by value under Object.is, avoiding
// an unstable useSyncExternalStore render loop.
function getSnapshot(): ChatSendMode {
  return parseMode(getLocalStorageValue(CHAT_SEND_MODE_KEY));
}

// The server has no localStorage, so render the default first;
// hydration then replaces it with the browser snapshot.
function getServerSnapshot(): ChatSendMode {
  return DEFAULT_CHAT_SEND_MODE;
}

export function useChatSendMode(): ChatSendMode {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function setChatSendMode(mode: ChatSendMode) {
  setLocalStorageValue(CHAT_SEND_MODE_KEY, mode);
  for (const listener of listeners) listener();
}

// Determine whether this keystroke submits the input.
// IME composition (isComposing or keyCode 229) must pass through,
// otherwise Enter used to confirm a composed character would submit accidentally.
// Enter mode preserves the prior Shift-only newline exception;
// Ctrl+Enter mode accepts both Ctrl and macOS Cmd.
// The default preference therefore retains the existing keyboard behavior.
export function shouldSubmitOnKey(e: React.KeyboardEvent, mode: ChatSendMode): boolean {
  if (e.key !== "Enter") return false;
  if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return false;
  if (mode === "ctrl-enter") return e.ctrlKey || e.metaKey;
  return !e.shiftKey;
}

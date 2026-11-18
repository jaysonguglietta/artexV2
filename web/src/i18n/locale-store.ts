"use client";
import type { Locale } from "./config";
// Compatibility functions for components originally written for multiple locales.
export function useActiveLocale(): Locale {
  return "en";
}
export function setActiveLocale(_locale: Locale): void {
  /* English is fixed. */
}

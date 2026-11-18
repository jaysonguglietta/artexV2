// This edition is English-only. Saved preferences from older releases fall back to English.
export const LOCALES = ["en"] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = "en";
export const LOCALE_LABELS: Record<Locale, string> = { en: "English" };
export const LOCALE_COOKIE = "artex_locale";
export const LOCALE_STORAGE_KEY = "artex_locale";
export function isLocale(raw: unknown): raw is Locale {
  return raw === "en";
}
export function resolveLocale(): Locale {
  return "en";
}

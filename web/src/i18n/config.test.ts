import { describe, expect, it } from "vitest";

import { DEFAULT_LOCALE, isLocale, LOCALE_LABELS, LOCALES, resolveLocale } from "./config";

describe("English-only configuration", () => {
  it("exposes only English", () => {
    expect([...LOCALES]).toEqual(["en"]);
    expect(DEFAULT_LOCALE).toBe("en");
    expect(LOCALE_LABELS.en).toBe("English");
  });
  it("rejects old locale preferences", () => {
    for (const value of ["ko", "zh", "es", "ja", null, undefined, 1]) expect(isLocale(value)).toBe(false);
    expect(isLocale("en")).toBe(true);
  });
  it("always renders English", () => {
    expect(resolveLocale()).toBe("en");
  });
});

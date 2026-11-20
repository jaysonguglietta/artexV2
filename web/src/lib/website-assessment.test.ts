import { describe, expect, it } from "vitest";

import { limitedWebsiteAssessment, shellQuote, targetFromDescription, websiteTarget } from "./website-assessment";

describe("guided website boundaries", () => {
  it("accepts a bare hostname and preserves the exact host", () => {
    expect(websiteTarget("gugnet.us").href).toBe("https://gugnet.us/");
    const task = limitedWebsiteAssessment("https://www.example.com/start");
    expect(task.description).toContain("HTTPS requests to www.example.com only");
    expect(task.description).toContain("at most five");
    expect(task.goal).toContain("blocked or incomplete");
  });
  it.each([
    "",
    "http://example.com",
    "https://user:pass@example.com",
    "file:///etc/passwd",
    "https://example.com:8443",
    "https://example.com/?token=secret",
    "https://example.com/#secret",
  ])("rejects unsupported or sensitive URL %s", (value) => {
    expect(() => websiteTarget(value)).toThrow();
  });
  it("does not turn untrusted text into an executable shell argument", () => {
    expect(shellQuote("a'$(touch /tmp/unwanted)")).toBe("'a'\\''$(touch /tmp/unwanted)'");
    expect(targetFromDescription("No website here")).toBe("");
    expect(targetFromDescription("Target https://example.com/\nScope")).toBe("https://example.com/");
  });
});

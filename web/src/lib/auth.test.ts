import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { auth } from "./auth";

describe("cookie session security", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubEnv("NEXT_PUBLIC_MOCK", "0");
  });
  afterEach(() => vi.unstubAllGlobals());

  it("never persists bearer credentials in browser storage or readable cookies", () => {
    localStorage.setItem("artex_token", "legacy-secret");
    auth.setToken("new-secret");
    expect(localStorage.getItem("artex_token")).toBeNull();
    expect(JSON.stringify(localStorage)).not.toContain("new-secret");
    expect(document.cookie).not.toContain("new-secret");
    expect(auth.getToken()).toBe("cookie-session");
  });

  it("rejects a stale UI marker when the server denies the session", async () => {
    auth.setToken("not-stored");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 401 })));
    expect(await auth.checkSession()).toBe(false);
    expect(auth.getToken()).toBeNull();
  });

  it("does not falsely report successful sign-out when revocation fails", async () => {
    auth.setToken("not-stored");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 503 })));
    await expect(auth.clearToken()).rejects.toThrow("Sign-out failed");
    expect(auth.getToken()).toBe("cookie-session");
  });

  it("waits for server revocation before clearing authenticated UI state", async () => {
    auth.setToken("not-stored");
    const request = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", request);
    await auth.clearToken();
    expect(request).toHaveBeenCalledWith("/api/auth/logout", { method: "POST", credentials: "same-origin" });
    expect(auth.getToken()).toBeNull();
  });
});

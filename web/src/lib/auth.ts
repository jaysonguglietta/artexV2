// Authentication uses a server-set HttpOnly cookie. No bearer token is stored
// in browser storage or readable cookies. A marker only controls initial UI state.
const SESSION_MARKER = "artex_authenticated";
export interface CurrentUser {
  id: string;
  name: string;
  username: string;
  email: string;
  avatar: string;
  role: string;
}
export const auth = {
  getToken(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem(SESSION_MARKER) === "1"
      ? "cookie-session"
      : process.env.NEXT_PUBLIC_MOCK === "1"
        ? "mock-demo"
        : null;
  },
  setToken(_token: string): void {
    localStorage.removeItem("artex_token");
    localStorage.setItem(SESSION_MARKER, "1");
  },
  async clearToken(): Promise<void> {
    if (process.env.NEXT_PUBLIC_MOCK !== "1") {
      const response = await fetch("/api/auth/logout", { method: "POST", credentials: "same-origin" });
      if (!response.ok && response.status !== 401)
        throw new Error("Sign-out failed. Retry before closing this session.");
    }
    localStorage.removeItem("artex_token");
    localStorage.removeItem(SESSION_MARKER);
  },
  async checkSession(): Promise<boolean> {
    if (process.env.NEXT_PUBLIC_MOCK === "1") return true;
    const response = await fetch("/api/auth/session", { credentials: "same-origin" });
    if (response.ok) {
      localStorage.setItem(SESSION_MARKER, "1");
      return true;
    }
    if (response.status === 401) {
      localStorage.removeItem(SESSION_MARKER);
      return false;
    }
    throw new Error("Cannot verify your session. Check the server connection.");
  },
  getCurrentUser(): CurrentUser | null {
    if (!this.getToken()) return null;
    return { id: "1", name: "ARTEX", username: "ARTEX", email: "", avatar: "", role: "operator" };
  },
};

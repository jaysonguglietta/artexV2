// Explicit mock switch injected at build time; only NEXT_PUBLIC variables reach the browser.
// NEXT_PUBLIC_MOCK=1 uses synthetic routes without a backend.
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";

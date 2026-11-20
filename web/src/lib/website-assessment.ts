/** A deliberately narrow starter assessment; prompts do not replace the broker boundary. */
export function websiteTarget(value: string): URL {
  let url: URL;
  try {
    url = new URL(value.includes("://") ? value.trim() : `https://${value.trim()}`);
  } catch {
    throw new Error("Enter a website address, such as https://example.com.");
  }
  if (url.protocol !== "https:" || url.username || url.password || !url.hostname || (url.port && url.port !== "443")) {
    throw new Error("Use an HTTPS website without a username, password, or custom port.");
  }
  if (url.search || url.hash) throw new Error("Use a public page address without query parameters or a # fragment.");
  return url;
}

export function limitedWebsiteAssessment(value: string) {
  const url = websiteTarget(value);
  return {
    name: `${url.hostname} — website check`,
    description: `Perform a limited, non-destructive assessment of ${url.href}\n\nScope: HTTPS requests to ${url.host} only. Publicly accessible pages only. Other hosts, subdomains, third-party services, and cross-host redirects are excluded.\n\nStart with one certificate-validated homepage GET without retries, redirects, or subresources. Record TLS details, HTTP status, security headers and cookie attributes with values redacted. Then inspect at most five ordinary same-host public page links. Use sequential requests at least one second apart.\n\nDo not brute-force credentials, fuzz paths, scan ports, submit forms, create accounts, upload files, exploit weaknesses, or retrieve sensitive data. Stop on HTTP 429, repeated server errors, or signs of disruption. Request operator approval before intrusive testing. These instructions do not replace network policy enforcement.`,
    goal: "Produce an evidence-backed website baseline. List checks actually performed, their results, affected URLs, and limitations. Distinguish confirmed vulnerabilities from recommendations and unverified suspicions. If tools or target access fail, report testing as blocked or incomplete; never treat a blocked check as a successful assessment. Zero findings does not establish that the website is secure.",
  };
}

export function targetFromDescription(description: string): string {
  const candidate = description.match(/https:\/\/[^\s<>"']+/)?.[0];
  if (!candidate) return "";
  try {
    return websiteTarget(candidate).href;
  } catch {
    return "";
  }
}

export function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

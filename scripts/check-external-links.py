#!/usr/bin/env python3
"""Check external HTTP(S) references in repository Markdown documents.

The companion check-doc-links.py checks local files and anchors without using the
network. This separate, manual or scheduled check can detect moved, restricted or
unavailable public reference pages. It is not a required offline merge check.

URL selection:
- Read tracked .md files in Git checkouts, or source .md files in an archive,
  excluding dependency/runtime trees, fenced code blocks and inline code spans.
  URLs in those spans are examples, configuration values or quoted evidence,
  rather than reference links readers are expected to visit.
- Collect Markdown links/images, autolinks and bare HTTP(S) URLs in prose.
- Skip recognized placeholder hosts and literal private/loopback addresses:
  localhost, common private IPv4 ranges, ::1, example domains, .test, .invalid,
  .local, .tld and names without a dot. This filter is not an SSRF boundary;
  review document changes before running a networked check. DNS and redirects
  are handled by urllib and are not constrained to public addresses.

Requests use GET, a browser User-Agent and ordinary redirect handling because
some reference sites reject HEAD or default client headers. Classifications:
- OK: a successful final response.
- RESTRICTED: HTTP 401, 403, 405 or 429. The host responded but blocked this
  request; report the restriction without failing strict mode.
- DOWN: other HTTP errors, or network/TLS errors after bounded retries.
  These need review; they do not establish that a site is permanently gone.
Network errors, HTTP 408 and 5xx responses receive brief delayed retries.
Only Python's standard library is required.

The allowlist records unavailable historical attribution/reference URLs we do
not control. DOWN links on it become ALLOWED and do not fail strict mode.
Translated upstream history retains its version/date/contributor references;
English conversion does not justify deleting attribution or rewriting raw
technical evidence. Fix newly broken curated links instead of allowlisting them.

Usage:
- Report only: python3 -I scripts/check-external-links.py
- Fail for DOWN links outside the allowlist: add --strict.
- Print the extracted URLs and locations without requests: add --list.
"""
import argparse
import http.client
import os
import re
import socket
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from urllib.parse import urlsplit

# Markdown links and images; stop the URL at whitespace or a closing parenthesis.
MD_LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)")
MD_IMAGE = re.compile(r"!\[[^\]]*\]\(([^)\s]+)")
# Angle-bracket autolinks.
AUTOLINK = re.compile(r"<(https?://[^>\s]+)>")
# Bare prose URLs; trim trailing punctuation after extraction.
BARE_URL = re.compile(r"https?://[^\s)>\]\"'`]+")
# Replace single-backtick inline code spans with whitespace before extraction.
INLINE_CODE = re.compile(r"`[^`]*`")
# Common punctuation following a prose URL.
TRAILING_PUNCT = ".,;:!?\"'»)]}>"

# Common private, loopback and link-local IPv4 prefixes; handle 172.16/12 separately.
PRIVATE_IPV4 = re.compile(r"^(127\.|10\.|192\.168\.|169\.254\.|0\.0\.0\.0$)")
PRIVATE_IPV4_172 = re.compile(r"^172\.(1[6-9]|2\d|3[01])\.")

BROWSER_UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"
)

# A responding host restricted the request; report this separately from DOWN.
RESTRICTED_CODES = {401, 403, 405, 429}

ALLOWLIST_PATH = os.path.join("scripts", "external-links-allowlist.txt")


def load_allowlist(root: str) -> set:
    """Read known unavailable historical URLs; return an empty set if absent.

    One URL per line. Ignore empty lines and text following a # comment marker.
    """
    path = os.path.join(root, ALLOWLIST_PATH)
    allow = set()
    if not os.path.exists(path):
        return allow
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.split("#", 1)[0].strip()
            if line:
                allow.add(line)
    return allow


def strip_code(line: str) -> str:
    """Remove inline code from a nonfenced line before extracting references."""
    return INLINE_CODE.sub(" ", line)


def is_checkable(url: str) -> bool:
    """Skip recognized placeholders and literal private/loopback hosts."""
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https"):
        return False
    host = parts.hostname
    if not host:
        return False
    host = host.lower()
    if host == "localhost" or host.endswith(".localhost"):
        return False
    if host == "::1":
        return False
    if PRIVATE_IPV4.match(host) or PRIVATE_IPV4_172.match(host):
        return False
    if host in ("example.com", "example.org", "example.net", "example.edu"):
        return False
    if host.endswith((".example.com", ".example.org", ".example.net", ".example")):
        return False
    if host.endswith((".test", ".invalid", ".local", ".localdomain", ".tld")):
        return False
    if "." not in host:  # A bare non-FQDN name, such as target.
        return False
    return True


def extract(root: str, md_files: list) -> dict:
    """Map reference URLs to their tracked Markdown file/line locations."""
    found = defaultdict(list)
    for md in md_files:
        with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
            lines = fh.readlines()
        in_fence = False
        for lineno, raw in enumerate(lines, 1):
            stripped = raw.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            line = strip_code(raw)
            candidates = []
            for pattern in (MD_LINK, MD_IMAGE, AUTOLINK):
                candidates.extend(m.group(1) for m in pattern.finditer(line))
            for m in BARE_URL.finditer(line):
                candidates.append(m.group(0).rstrip(TRAILING_PUNCT))
            for url in candidates:
                url = url.strip().rstrip(TRAILING_PUNCT)
                if is_checkable(url):
                    where = (md, lineno)
                    if where not in found[url]:
                        found[url].append(where)
    return found


def probe(url: str, timeout: float, retries: int, delay: float):
    """GET a URL with browser headers and return (classification, detail).

    Classifications are OK, RESTRICTED and DOWN. Retry network errors, 408 and 5xx.
    """
    req = urllib.request.Request(
        url,
        method="GET",
        headers={
            "User-Agent": BROWSER_UA,
            "Accept": "*/*",
            "Accept-Language": "en",
        },
    )
    last = ""
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                code = resp.getcode()
                if code in RESTRICTED_CODES:
                    return "RESTRICTED", f"HTTP {code}"
                return "OK", f"HTTP {code}"
        except urllib.error.HTTPError as exc:
            code = exc.code
            if code in RESTRICTED_CODES:
                return "RESTRICTED", f"HTTP {code}"
            if code >= 500 or code == 408:
                last = f"HTTP {code}"  # Retry potentially temporary failures.
            else:
                return "DOWN", f"HTTP {code}"  # Do not retry other HTTP errors, including 404/410.
        except (
            urllib.error.URLError,
            http.client.HTTPException,
            ssl.SSLError,
            socket.timeout,
            ConnectionError,
            TimeoutError,
            OSError,
        ) as exc:
            reason = getattr(exc, "reason", exc)
            last = f"{type(exc).__name__}: {reason}"
        if attempt < retries:
            time.sleep(delay * (attempt + 1))
    return "DOWN", last


def markdown_files(root: str):
    """Use tracked files in a checkout, with an offline source-archive fallback."""
    if os.path.exists(os.path.join(root, ".git")):
        tracked = subprocess.check_output(
            ["git", "ls-files", "-z"], cwd=root, text=True
        ).split("\0")
        return sorted(f for f in tracked if f.endswith(".md")), "tracked"
    ignored = {
        "node_modules", ".next", ".git", "out", "dist", "__pycache__", "data",
        "work", "private", "bench", ".cache", ".swc-cache", ".aws", ".ssh",
        ".gnupg", ".docker", ".secrets",
    }
    found = []
    for directory, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = [name for name in dirs if name not in ignored
                   and not os.path.islink(os.path.join(directory, name))]
        found.extend(os.path.relpath(os.path.join(directory, name), root)
                     for name in files if name.endswith(".md")
                     and not os.path.islink(os.path.join(directory, name)))
    return sorted(found), "source"


def main() -> int:
    ap = argparse.ArgumentParser(description="Check external links in repository Markdown")
    ap.add_argument("--strict", action="store_true",
                    help="Exit 1 for DOWN links outside the allowlist (scheduled/release checks)")
    ap.add_argument("--list", action="store_true",
                    help="List reference URLs and locations without network requests")
    ap.add_argument("--timeout", type=float, default=15.0, help="Request timeout in seconds")
    ap.add_argument("--retries", type=int, default=2, help="Retry count for network errors, HTTP 408 and 5xx")
    ap.add_argument("--delay", type=float, default=0.5, help="Base delay between requests and retries in seconds")
    args = ap.parse_args()

    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    md_files, source_kind = markdown_files(root)

    allow = load_allowlist(root)
    found = extract(root, md_files)
    urls = sorted(found)
    print(f"Collected {len(urls)} external reference URLs from {len(md_files)} {source_kind} Markdown files")

    if args.list:
        for url in urls:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            tag = " [allowlist]" if url in allow else ""
            print(f"  {url}{tag}  ({where})")
        return 0

    results = {"OK": [], "RESTRICTED": [], "ALLOWED": [], "DOWN": []}
    for i, url in enumerate(urls):
        if i:
            time.sleep(args.delay)  # Space requests to reduce load on reference sites.
        verdict, detail = probe(url, args.timeout, args.retries, args.delay)
        if verdict == "DOWN" and url in allow:
            verdict = "ALLOWED"  # A documented unavailable historical upstream reference.
        results[verdict].append((url, detail))
        print(f"  [{verdict:10}] {url} — {detail}")

    print(
        f"\nSummary: OK {len(results['OK'])} · RESTRICTED {len(results['RESTRICTED'])} · "
        f"ALLOWED {len(results['ALLOWED'])} · DOWN {len(results['DOWN'])}"
    )
    if results["RESTRICTED"]:
        print("RESTRICTED (host responded but restricted this request):")
        for url, detail in results["RESTRICTED"]:
            print(f"  {url} — {detail}")
    if results["ALLOWED"]:
        print("ALLOWED (documented unavailable references excluded from strict failure):")
        for url, detail in results["ALLOWED"]:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            print(f"  {url} — {detail}  (references: {where})")
    if results["DOWN"]:
        print("DOWN (possibly unavailable; review required):")
        for url, detail in results["DOWN"]:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            print(f"  {url} — {detail}  (references: {where})")

    if args.strict and results["DOWN"]:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

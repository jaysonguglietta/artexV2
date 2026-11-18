#!/usr/bin/env python3
"""Check repository Markdown file, image and GitHub-style heading references.

Fenced code examples and external URLs are excluded. This deterministic check
uses only the Python standard library and works with Git checkouts and source
archives. It returns nonzero when an internal file or heading is missing.
"""
import os
import re
import subprocess
import sys
import unicodedata
from collections import defaultdict

# Markdown links, excluding images; optional link titles are not part of the path.
MD_LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)")
# Markdown images.
MD_IMAGE = re.compile(r"!\[[^\]]*\]\(([^)\s]+)")
# HTML image sources.
HTML_IMAGE = re.compile(r"<img[^>]*\bsrc=[\"']([^\"']+)[\"']", re.IGNORECASE)

# Non-file schemes; a bare fragment refers to the current document.
EXTERNAL_PREFIXES = ("http://", "https://", "mailto:", "tel:", "data:")

ATX_HEADING = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")
# Remove inline Markdown from headings before calculating slugs.
MD_INLINE_LINK = re.compile(r"\[([^\]]*)\]\([^)]*\)")


def is_external(target: str) -> bool:
    return target.startswith(EXTERNAL_PREFIXES)


def _is_word_char(ch: str) -> bool:
    """Recognize Unicode word characters retained in GitHub heading slugs."""
    if ch == "_":
        return True
    if ch in ("‌", "‍"):
        return True
    category = unicodedata.category(ch)
    if category[0] in ("L", "M"):
        return True
    return category in ("Nd", "Pc")


def slugify(text: str) -> str:
    """Generate a heading slug, before duplicate suffixes are assigned."""
    text = text.strip().lower()
    out = []
    for ch in text:
        if ch == " ":
            out.append("-")
        elif ch == "-" or _is_word_char(ch):
            out.append(ch)
    return "".join(out)


def heading_slugs(root: str, md: str) -> set:
    """Collect ATX heading slugs with GitHub-compatible duplicate suffixes."""
    slugs = set()
    counts = defaultdict(int)
    in_fence = False
    with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
        for line in fh:
            stripped = line.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            match = ATX_HEADING.match(line.rstrip("\n"))
            if not match:
                continue
            raw = match.group(2)
            raw = MD_INLINE_LINK.sub(r"\1", raw)
            raw = raw.replace("`", "").replace("*", "").replace("_", "")
            base = slugify(raw)
            seen = counts[base]
            counts[base] += 1
            slugs.add(base if seen == 0 else f"{base}-{seen}")
    return slugs


def main() -> int:
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    ignored = {"node_modules", ".next", ".git", "out", "dist", "__pycache__", "data", "work"}
    tracked = []
    for directory, dirs, files in os.walk(root):
        dirs[:] = [d for d in dirs if d not in ignored]
        tracked.extend(os.path.relpath(os.path.join(directory, name), root) for name in files)
    md_files = [f for f in tracked if f.endswith(".md")]

    slug_cache: dict = {}

    def slugs_of(md_path: str) -> set:
        if md_path not in slug_cache:
            slug_cache[md_path] = heading_slugs(root, md_path)
        return slug_cache[md_path]

    checked_files = 0
    checked_anchors = 0
    broken_files = []
    broken_anchors = []
    for md in md_files:
        with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
            lines = fh.readlines()
        base_dir = os.path.dirname(md)
        in_fence = False
        for lineno, line in enumerate(lines, 1):
            stripped = line.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            for pattern in (MD_LINK, MD_IMAGE, HTML_IMAGE):
                for match in pattern.finditer(line):
                    target = match.group(1).strip()
                    if is_external(target):
                        continue

                    if target.startswith("#"):
                        file_part, anchor = "", target[1:]
                    elif "#" in target:
                        file_part, anchor = target.split("#", 1)
                    else:
                        file_part, anchor = target, ""
                    file_part = file_part.split("?", 1)[0]
                    anchor = anchor.strip()

                    # Anchor target: the linked Markdown file, or the current document.
                    target_md = None
                    if file_part:
                        checked_files += 1
                        resolved = os.path.normpath(
                            os.path.join(root, base_dir, file_part)
                        )
                        if not os.path.exists(resolved):
                            broken_files.append((md, lineno, target))
                            continue
                        if file_part.endswith(".md"):
                            target_md = os.path.normpath(
                                os.path.join(base_dir, file_part)
                            )
                    else:
                        target_md = md

                    # Fragments in image URLs are not heading references.
                    if anchor and target_md is not None and pattern is MD_LINK:
                        checked_anchors += 1
                        if anchor not in slugs_of(target_md):
                            broken_anchors.append((md, lineno, target, target_md))

    print(
        f"Markdown files: {len(md_files)}; file references: {checked_files}; "
        f"heading references: {checked_anchors}"
    )
    if broken_files:
        print(f"Broken file references ({len(broken_files)}):")
        for md, lineno, target in broken_files:
            print(f"  {md}:{lineno} -> {target}")
    if broken_anchors:
        print(f"Broken heading references ({len(broken_anchors)}):")
        for md, lineno, target, target_md in broken_anchors:
            print(f"  {md}:{lineno} -> {target}  (target: {target_md})")
    if broken_files or broken_anchors:
        return 1
    print("All internal file, image and heading references resolve.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

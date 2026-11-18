#!/usr/bin/env python3
"""Reject unintended CJK characters in statically exported English HTML.

This checks generated HTML only, not intentional locale catalogs or source code.
No external service is contacted. An optional argument specifies the export root.
"""
import os
import re
import sys

HAN = re.compile(r"[\u3400-\u9fff\uf900-\ufaff\uac00-\ud7af\u3040-\u30ff\U00020000-\U0002ffff]")
ALLOWED_HAN: set = set()


def find_out_dir() -> str:
    if len(sys.argv) > 1:
        return os.path.abspath(sys.argv[1])
    return os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "web", "out")


def main() -> int:
    out_dir = find_out_dir()
    if not os.path.isdir(out_dir):
        print(f"Export directory missing: {out_dir}. Run npm run build:static first.")
        return 2
    checked = 0
    offenders = []
    for directory, _dirs, files in os.walk(out_dir):
        for name in files:
            if not name.endswith(".html"):
                continue
            checked += 1
            path = os.path.join(directory, name)
            with open(path, encoding="utf-8", errors="ignore") as source:
                found = [ch for ch in HAN.findall(source.read()) if ch not in ALLOWED_HAN]
            if found:
                offenders.append((os.path.relpath(path, out_dir), len(found), "".join(sorted(set(found)))[:30]))
    print(f"Checked {checked} HTML files in {out_dir}")
    for path, count, preview in offenders:
        print(f"  {path}: {count} unexpected CJK characters ({preview})")
    if offenders:
        print("Translate unintended visible text; document any intentional allowlist exception.")
        return 1
    if checked == 0:
        print("No HTML files found. Verify the export completed.")
        return 2
    print("No unintended CJK characters in the English export.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

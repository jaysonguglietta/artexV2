#!/usr/bin/env python3
"""Inventory remaining CJK text for component-level English conversion.

Matches include raw evidence, legacy identifiers, and Unicode test inputs, so
this is a review queue rather than proof that every match should be translated."""
import json
import pathlib
import re
import sys
root = pathlib.Path(__file__).resolve().parent.parent
cjk = re.compile(r"[\u3400-\u9fff\uac00-\ud7af\u3040-\u30ff]")
skip = {"node_modules", ".next", ".git", "out", "dist", "__pycache__"}
rows = []
for path in sorted(root.rglob("*")):
    if not path.is_file() or skip.intersection(path.relative_to(root).parts) or any(part.startswith(".scratch.") for part in path.relative_to(root).parts):
        continue
    try:
        content = path.read_text(encoding="utf-8")
    except (UnicodeError, OSError):
        continue
    matches = [(i, len(cjk.findall(line))) for i, line in enumerate(content.splitlines(), 1) if cjk.search(line)]
    if not matches:
        continue
    rel = str(path.relative_to(root))
    localized = rel.startswith("web/messages/") or any(x in path.name for x in (".ko.", ".zh.", ".ja.", "-ko.", "-zh."))
    rows.append({"path": rel, "category": "localized-resource" if localized else "source-or-documentation", "characters": sum(n for _, n in matches), "lines": [i for i, _ in matches]})
json.dump({"files_remaining": len(rows), "files": rows}, sys.stdout, indent=2)
print()

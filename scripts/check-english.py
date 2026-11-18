#!/usr/bin/env python3
"""Reject unreviewed CJK source text while preserving exact technical fixtures.

Exceptions bind an individual line to its path, line number, hash and reason.
Any changed/new match needs explicit review; no blanket directory exemptions.
Publication exclusions keep private runtime/configuration out of this check.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys

PATTERN = re.compile(r'[\u3400-\u9fff\uac00-\ud7af\u3040-\u30ff]')


def verify(root):
    expected = {}
    for name in ['translation-exceptions.json', 'translation-go-exceptions.json']:
        for entry in json.loads((root / 'docs' / name).read_text())['entries']:
            key = (entry['path'], entry['line'])
            if key in expected or not entry.get('reason', '').strip():
                raise ValueError('Duplicate or unexplained translation exception: ' + str(key))
            expected[key] = entry['line_sha256']
    spec = importlib.util.spec_from_file_location('prepare_upload', root / 'scripts' / 'prepare-upload.py')
    publication = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(publication)
    found, problems = set(), []
    for relative, data, _mode in publication.read_source(root):
        try:
            content = data.decode('utf-8')
        except UnicodeDecodeError:
            continue
        for number, line in enumerate(content.splitlines(), 1):
            if not PATTERN.search(line):
                continue
            key = (str(relative), number)
            found.add(key)
            actual = hashlib.sha256(line.encode()).hexdigest()
            if expected.get(key) != actual:
                problems.append(f'{relative}:{number}: new or changed non-English text needs review')
    for key in expected.keys() - found:
        problems.append(f'{key[0]}:{key[1]}: stale translation exception needs review')
    return len(found), problems


def main():
    root = Path(__file__).resolve().parent.parent
    count, problems = verify(root)
    for problem in problems:
        print(problem)
    print(f'Reviewed exact technical-data lines: {count}; unreviewed/stale matches: {len(problems)}')
    return 1 if problems else 0

if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError) as error:
        print('English conversion verification failed: ' + str(error), file=sys.stderr)
        sys.exit(2)

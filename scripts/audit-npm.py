#!/usr/bin/env python3
"""Report npm results, distinguishing independently verified withdrawn advisories.

Never ignore all alerts on a package or severity. Every advisory leaf needs either
an upstream withdrawal or a fix. Offline/API failures fail closed.
"""
import json
from pathlib import Path
import subprocess
import sys
import urllib.request


def classify(report, withdrawn):
    vulnerabilities = report.get('vulnerabilities', {})
    active, resolved = set(), set()

    def visit(name, seen):
        if name in seen or name not in vulnerabilities:
            active.add('unresolved dependency: ' + name)
            return
        for item in vulnerabilities[name].get('via', []):
            if isinstance(item, str):
                visit(item, seen | {name})
            else:
                url = item.get('url', '')
                if withdrawn.get(url):
                    resolved.add(url)
                else:
                    active.add(url or 'unknown advisory: ' + name)
        if not vulnerabilities[name].get('via'):
            active.add('missing advisory evidence: ' + name)

    for name in vulnerabilities:
        visit(name, set())
    return sorted(active), sorted(resolved)


def main():
    root = Path(__file__).resolve().parent.parent
    result = subprocess.run(['npm', 'audit', '--json'], cwd=root / 'web', capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise RuntimeError('npm audit failed; no clean result is available')
    report = json.loads(result.stdout)
    if 'error' in report or 'vulnerabilities' not in report:
        raise RuntimeError('npm did not return a complete advisory report')
    urls = {item.get('url', '') for entry in report['vulnerabilities'].values()
            for item in entry.get('via', []) if isinstance(item, dict)}
    withdrawn = {}
    for url in sorted(urls):
        prefix = 'https://github.com/advisories/'
        if not url.startswith(prefix):
            continue
        advisory_id = url[len(prefix):]
        if not advisory_id.startswith('GHSA-') or any(ch not in 'GHSA-0123456789abcdefghijklmnopqrstuvwxyz' for ch in advisory_id):
            continue
        request = urllib.request.Request('https://api.github.com/advisories/' + advisory_id,
                                         headers={'Accept': 'application/vnd.github+json', 'User-Agent': 'ARTEX-dependency-review'})
        with urllib.request.urlopen(request, timeout=15) as response:
            record = json.load(response)
        if record.get('ghsa_id') != advisory_id:
            raise RuntimeError('Advisory identity mismatch')
        withdrawn[url] = record.get('withdrawn_at')
    active, withdrawn_urls = classify(report, withdrawn)
    summary = {'raw_npm_counts': report['metadata']['vulnerabilities'],
               'verified_withdrawn': {url: withdrawn[url] for url in withdrawn_urls},
               'active_or_unverified': active}
    print(json.dumps(summary, indent=2))
    return 1 if active else 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        print('Dependency verification failed: ' + str(error), file=sys.stderr)
        sys.exit(2)

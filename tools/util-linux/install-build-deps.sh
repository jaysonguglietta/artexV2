#!/bin/sh
# Use the architecture's HTTPS Ubuntu archive and its signed Sources metadata.
set -eu
export DEBIAN_FRONTEND=noninteractive
sed -i 's/^Types: deb$/Types: deb deb-src/' /etc/apt/sources.list.d/ubuntu.sources
apt-get update
apt-get install -y --no-install-recommends build-essential dpkg-dev python3 quilt
# The package archive pin must also be present in the signed Ubuntu Sources
# index. This checks archive origin separately from the upstream tar signature.
python3 - <<'PYCODE'
import json
from pathlib import Path
import subprocess

wanted = 'util-linux_2.41.3-3ubuntu2.2.debian.tar.xz'
expected = 'e8f366168a1483616c1baf06420e341789340dcd2a98b9d8423f076d6cffa043'
text = subprocess.run(['apt-cache', 'showsrc', '--only-source', 'util-linux'],
                      text=True, capture_output=True, check=True).stdout
matches = []
for paragraph in text.split('\n\n'):
    fields = {}
    key = None
    for line in paragraph.splitlines():
        if line.startswith((' ', '\t')):
            if key is not None:
                fields[key] += '\n' + line.strip()
        elif ':' in line:
            key, value = line.split(':', 1)
            fields[key] = value.strip()
        else:
            key = None
    if fields.get('Package') != 'util-linux' or fields.get('Version') != '2.41.3-3ubuntu2.2':
        continue
    for row in fields.get('Checksums-Sha256', '').splitlines():
        columns = row.split()
        if len(columns) == 3 and columns[2] == wanted:
            matches.append(columns)
if not matches or any(row != [expected, '125404', wanted] for row in matches):
    raise SystemExit('Pinned Ubuntu packaging is absent or mismatched in authenticated APT Sources')
proof = {'method': 'APT InRelease and Sources verification', 'source': 'util-linux',
         'version': '2.41.3-3ubuntu2.2', 'filename': wanted, 'sha256': expected,
         'size': 125404}
Path('/opt/artex-util-linux/ubuntu-source-authentication.json').write_text(json.dumps(proof, indent=2) + '\n')
print('Pinned Ubuntu packaging authenticated against the signed APT Sources index')
PYCODE
apt-get build-dep -y --no-install-recommends --arch-only -Pnoudeb util-linux=2.41.3-3ubuntu2.2

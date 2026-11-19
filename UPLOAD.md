# Prepare and upload ARTEX V2 source

The intended personal repository is
[jaysonguglietta/artexV2](https://github.com/jaysonguglietta/artexV2).
Its HTTPS remote is `https://github.com/jaysonguglietta/artexV2.git`.
These are reusable upload and recovery instructions, not a claim that a push has
already completed. The [product readiness review](docs/PRODUCT-READINESS-REVIEW.md)
separates verified feature slices from unverified journeys and proposed improvements.
Implementation `375b2c3f460d30cddfecd3437e47b543ec5b9b66` passed all six Go
jobs, eight native reviewed-image gates, documentation and the real main OIDC
rehearsal, then reached main through a fast-forward. The final post-edit source
snapshot/credential scan remains separate in [validation](docs/VALIDATION.md).
No version tag, formal release, package or enabling variable was created.

The jq/util-linux remediation authorized after the `3062e9c…` baseline is in
progress. Its fresh builds, native scans, worker regressions and source/archive
verification must complete before a new upload copy is labeled validated. Older
85 Medium/9 Low and `375b2c3…` results are historical. See
[worker security packages](docs/WORKER-SECURITY-PATCHES.md).

The prepared source copy excludes Git history, installed dependencies, compiled
binaries, local configuration, private keys, database state and runtime captures.
Original licensing and attribution remain. `SOURCE-MANIFEST.json` records the
prepared files and their hashes; subsequent edits require a new manifest and
review. Review [the security assessment](docs/SECURITY-REVIEW.md) and
[validation record](docs/VALIDATION.md) before publishing a changed snapshot.

## Upload a fresh source copy

Use an empty repository at the intended remote, or review any existing contents
before importing. Do not initialize a second README, license or `.gitignore`
through GitHub when uploading this complete source tree.

Extract the archive and open a terminal in the directory containing this file:

```sh
git init -b main
git add .
git status --short
git diff --cached --stat
git commit -m "Harden ARTEX, isolate tool execution, and use English"
git remote add origin https://github.com/jaysonguglietta/artexV2.git
git remote -v
git push -u origin main
```

Inspect the staged paths before committing. Keep `LICENSE` and
`THIRD_PARTY_NOTICES.md`; exclude `.env`, `.secrets`, worker policy, real provider
keys, logs, captures, database backups and generated artifacts. Local credentials
belong in private files, never in a commit, command argument or remote URL. Use
GitHub's normal credential manager or browser authentication. Git preserves
executable script modes; browser file uploads may lose them.

`scripts/prepare-upload.py` prepares a new source directory, deterministic ZIP,
checksum and manifest. It refuses existing output paths and recognized credential
material; it does not create a remote repository or publish anything. Its checks
supplement source review and a reputable secret scanner rather than proving every
possible secret format is absent.

## Existing checkout or rejected push

For an existing checkout, inspect its repository and remote before making changes:

```sh
git status
git remote -v
git log -5 --oneline
```

If `origin` is missing, add the exact remote above. If it points elsewhere, verify
that this is the intended checkout before using
`git remote set-url origin https://github.com/jaysonguglietta/artexV2.git`.
Do not replace another project's remote or copy private runtime data into it.

If GitHub rejects the push because `main` already contains commits, preserve that
history. Fetch and inspect the differences, then use a new review branch if an
import is still appropriate:

```sh
git fetch origin
git log --oneline --left-right HEAD...origin/main
git switch -c review/hardened-source
git push -u origin review/hardened-source
```

Review the branch before merging. Do not force-push over existing history to make
an upload succeed. A permission/authentication failure requires correcting account
access through GitHub; embedding a token in the URL is not a recovery method.
GitHub requires the `workflow` OAuth scope to upload changes under
`.github/workflows`. If the saved HTTPS sign-in lacks that scope, either refresh
the correct personal account's permissions or use an SSH key already registered
to that account:

```sh
ssh -T git@github.com
git remote set-url origin git@github.com:jaysonguglietta/artexV2.git
git push -u origin main
```

Confirm GitHub identifies the intended personal account. Its successful SSH test
says shell access is unavailable; that message is normal. Keep host-key
verification enabled and never upload a private SSH key or token into the repo.

If a real secret is ever committed, revoke it first and follow an explicit history
cleanup process; deleting its current file does not remove it from prior commits.

## Run locally

For chat and records without autonomous scanning:

```sh
cp .env.example .env
# Set unique POSTGRES_PASSWORD and ARTEX_ADMIN_PASSWORD in .env.
./scripts/setup-workers.sh --secret-only
docker compose build postgres
sh scripts/setup-postgres.sh
docker compose up --build -d
```

Open http://localhost:8787. For scanning, follow
[isolated task execution](docs/WORKER-ISOLATION.md): build the worker and broker,
create private policy/credentials, and authorize the task's exact target CIDRs.
The API has no local executable-tool fallback. Use a dedicated assessment VM.
The PostgreSQL build and preflight must precede startup. For existing database
volumes, follow the explicit offline migration in
[PostgreSQL capacity and backup operations](docs/POSTGRES-BACKUP.md); automatic
adoption is deliberately refused. That guide also covers host log activation and
encrypted backups verified through a disposable restore.

Stop services with the same Compose files and environment options used to start
them, followed by `down`. Database, signing-key and broker-state volumes remain
unless deliberately removed. Avoid `down -v` when preserving data.

See [the documentation index](docs/README.md) for architecture, operator workflows,
security controls and reproducible checks. Source upload does not require image or
release publishing. Before any later production release, verify the active
[release-tag rules](docs/RELEASE-TAG-PROTECTION.md), [signing identity](docs/SIGNED-RELEASES.md)
and [reviewed-findings image gate](docs/IMAGE-SECURITY.md). Any vendor-unfixed
Medium/Low allowance must be an exact, expiring, platform-specific worker Ubuntu
OS review with concrete containment; High/Critical/Unknown and fixable findings
remain blocking. Source publication neither enables nor bypasses release gates. The [npm](tools/npm/README.md) and
[pip](tools/python/README.md) forks must retain their honest identity and validation.
Passing checks does not certify safe unattended operation or
permission to test a target.

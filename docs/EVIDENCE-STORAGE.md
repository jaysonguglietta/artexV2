# Evidence storage, capture retention and legal holds

This single-operator deployment separates preserved finding evidence, disposable
raw network captures and workspace/archive files. Storage admission is enforced
before filesystem mutations, with a shared in-process mutex and an OS advisory
lock per bucket. Reservations include additional copies and temporary files;
concurrent uploads, imports and worker snapshots cannot independently spend the
same available capacity. Each admission recounts real files rather than trusting
a cached counter, so restarts and failed transactions do not reset usage.

These are independent application budgets, not a total VM disk limit. PostgreSQL,
its WAL, database-backed activity/model records, application logs, Docker images,
backups and filesystem allocation overhead need separate capacity controls.
Operator-managed source/skill directories outside the data directory also require
an explicit filesystem quota if made writable. The supported API image is
read-only outside its named volumes and bounded temporary filesystems.

## Defaults and configuration

Set these environment variables before starting the API. Values are positive
decimal integers in bytes or entries, without suffixes, signs or whitespace.
Malformed values fail startup even when traffic capture is disabled.

| Variable | Default | Meaning |
| --- | ---: | --- |
| `ARTEX_EVIDENCE_QUOTA_BYTES` | 2147483648 | 2 GiB preserved evidence body budget |
| `ARTEX_TRAFFIC_QUOTA_BYTES` | 5368709120 | 5 GiB raw capture/index budget, including temporary journal headroom |
| `ARTEX_WORKSPACE_QUOTA_BYTES` | 5368709120 | 5 GiB workspace, upload, archive and export admission budget |
| `ARTEX_EVIDENCE_QUOTA_ENTRIES` | 100000 | Evidence files plus directories |
| `ARTEX_TRAFFIC_QUOTA_ENTRIES` | 100000 | Traffic files plus directories |
| `ARTEX_WORKSPACE_QUOTA_ENTRIES` | 100000 | Workspace files plus directories |
| `ARTEX_MAX_EVIDENCE_BODY_BYTES` | 67108864 | 64 MiB maximum declared evidence body, checked before copying |
| `ARTEX_MAX_TRAFFIC_EXCHANGES` | 100000 | Maximum indexed raw exchanges |

Entry quotas cannot exceed one million. The evidence body limit must fit its
bucket. Traffic needs at least 1 MiB for its bounded SQLite index. File bytes and
entry counts are both limited: zero-byte files and directory trees still consume
metadata capacity. Small fixed-size lock/legal-hold control files have their own
bounded format and remain writable so holds can be released when a payload
bucket is full. Symlink/special-file anomalies fail write admission rather than
being followed outside a configured root. Windows write admission also rejects
alternate data streams and device aliases; pre-existing NTFS streams created
outside this application are not enumerable by these portable file counters and
need a separate filesystem inventory/quota.

Workspace accounting excludes the separate top-level `evidence/` and `traffic/`
buckets. It covers admitted writes to task/session/draft artifacts, private upload
staging, worker snapshot synchronization, archive packages/extraction/staging,
transcript archive copies and finding export staging. Generic workspace mutation
routes cannot modify evidence, traffic, archive or legal-hold internals, including
through a symlink alias. Read/download access remains authenticated.

SQLite uses rollback journaling rather than WAL. Every pooled connection receives
a page-count ceiling based on its actual page size. One quarter of the traffic
budget is the index ceiling; capture/import admission conservatively reserves
three times that ceiling plus 1 MiB for index/journal growth, in addition to new
blob bytes. This can reject new captures while the directory is visibly below its
nominal byte limit. The headroom is intentional: a WAL size hint cannot enforce
a hard growth bound. Rollback journaling also reduces read/write concurrency. Initial schema and CA
creation are reserved before writing. Complete existing indexes can open without
a new-write reservation so an over-budget store can still be read and reclaimed.
Deletion/rollback recovery may need bounded journal/control metadata even when
existing externally supplied files already exceed the payload budget; keep host
free-space headroom for recovery.

Existing over-budget stores remain readable and can be reclaimed. New writes
return HTTP **507 Insufficient Storage** with usage and an actionable explanation;
agent tools return an error result. A finding/binding transaction is not committed
when its required evidence cannot be preserved. Original files survive rejected
atomic uploads, and failed partial copies are removed or retained in explicitly
recoverable archive staging. Deletions are available without increasing an
admission budget, provided no applicable hold blocks them.

## Thirty-day raw capture retention

Raw network recorder exchanges expire after 30 days from their capture timestamp.
A bounded background pass runs periodically, deletes at most 500 expired rows per
batch, updates SQLite body/reference/full-text tables, and collects unreferenced
raw blobs. Legacy directory removal failures remain in a durable retry queue and
are reported; the system does not claim plaintext was removed when cleanup fails.
Large existing backlogs may need several passes. Automatic raw blob GC is deferred
while legacy `.http` rows remain, because their blob pointers are outside the
modern reference table. Status reports that compatibility limit and unknown legacy
delete stages for explicit operator review/purge; their bytes are not claimed
purged. Modern blob sweeps inspect at most 1,000 files per pass. Deletion is logical unlinking,
not a cryptographic erase guarantee for host snapshots or backups.

Finding-linked HTTP evidence is copied into the independent evidence store when
bound. Raw expiry and raw purge do not remove that copy. Evidence GC retains
referenced snapshots/bodies and normally gives unreferenced data a 24-hour grace
period. Its work is also batched. An explicit operator purge may remove an
unreferenced batch without that grace while holding the evidence transaction
lock; it still cannot remove a referenced body or bypass a hold.

Automatic TTL applies to the managed network recorder, not arbitrary uploaded
files, command output, reports, PostgreSQL records, backups or copies an operator
exports. Those may also contain sensitive information and require a case-specific
retention policy.

New task archives omit unbound raw network captures and mark
`raw_traffic_omitted: true`; finding-bound evidence and task artifacts remain in
the package. Archiving is not an escape hatch for indefinite raw capture storage.
Legacy archives may still contain raw captures: storage status reports packages
requiring operator review. They are preserved rather than silently rewritten or
deleted, because those packages may also hold the only finding evidence. Export
needed linked evidence and deliberately rebuild/review legacy packages. Restoring
an old package does not renew raw capture timestamps: expired raw exchanges are
skipped and future timestamps are rejected. Finding evidence restoration is
independent of expired raw rows.

## Durable legal holds

Holds are deliberately **whole-bucket**, identified by a random ID, a reason and
a creation time. Multiple holds may coexist; all must be released before a
blocked destructive operation can proceed. There are at most 64 active holds per
bucket, with bounded reason/state sizes. State is written atomically and synced
inside the bucket. Malformed state blocks deletion and requires operator repair.

Only authenticated operator HTTP routes create or release holds. Model-facing
tools cannot change them. Evidence holds stop both snapshot metadata and blob GC, binding removal/edits,
report replacement and destructive finding/task/archive operations. New findings
may still be appended; status/severity/name/class remain business metadata. Holds
are not a comprehensive immutable PostgreSQL record or a legal-compliance claim. Traffic holds stop expiry,
raw purge and the legacy host/task deletion staging paths. Workspace holds stop
replacement/deletion of existing admitted workspace artifacts, including durable
archive copies. Live ephemeral SDK debug transcript files are excluded: SDK appends
continue and a container restart destroys that tmpfs regardless of a workspace
hold. Preserve required debug data in an admitted durable artifact before stopping
the container. New evidence/captures may
continue within capacity; a hold does not authorize deleting protected data to
make space. Holds take effect when the request succeeds; an already committed
deletion cannot be retroactively preserved.

## Operator API

Use a native bearer token obtained from `/api/auth/login`, or the browser's
same-origin authenticated session. Never put the token in a URL or commit it to a
script. These requests are examples for a private local deployment:

```sh
# Read actual usage, limits, holds, capture/trace errors and legacy archive review counts.
curl -H "Authorization: Bearer $ARTEX_TOKEN" http://127.0.0.1:8787/api/storage

# Preserve the complete evidence bucket for a case.
curl -H "Authorization: Bearer $ARTEX_TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"evidence","reason":"Case review and preservation requirement"}' \
  http://127.0.0.1:8787/api/storage/holds

# Release a specific hold only after review; copy its ID from the status response.
curl -X DELETE -H "Authorization: Bearer $ARTEX_TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"evidence","confirmation":"RELEASE LEGAL HOLD"}' \
  http://127.0.0.1:8787/api/storage/holds/HOLD_ID

# Delete unheld raw captures, preserving finding-linked evidence copies.
curl -H "Authorization: Bearer $ARTEX_TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"traffic","confirmation":"DELETE UNHELD RAW CAPTURES"}' \
  http://127.0.0.1:8787/api/storage/purge

# Collect one unreferenced evidence batch, preserving every referenced body.
curl -H "Authorization: Bearer $ARTEX_TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"evidence","confirmation":"PURGE UNREFERENCED EVIDENCE"}' \
  http://127.0.0.1:8787/api/storage/purge
```

A hold refusal is HTTP 409. To recover from 507, inspect status, export needed
unheld data, remove expendable workspace artifacts or purge unheld raw captures,
then retry. Increase limits only after checking actual VM capacity. Releasing a
hold requires its own explicit confirmation; purge never releases holds.

## Raw SDK debug transcripts

Raw SDK transcript files are off by default for ordinary binaries. PostgreSQL
conversation/activity history remains the durable UI record; bounded history is
replayed for model context when raw files are disabled.

`ARTEX_RAW_TRANSCRIPTS=ephemeral` enables optional current-run raw debug traces
only after the API verifies Linux tmpfs capacity at most 256 MiB and at most 8192
inodes. Compose enables this mode by default and supplies the separate `noexec,nosuid,nodev` tmpfs at
`/app/data/transcripts`, owned by UID/GID 10001. That maximum capacity is reserved
from workspace admission headroom. Files disappear when the container restarts;
they are not a durable evidence backup. A deliberately created task archive may
copy them into its admitted package before shutdown. Optional raw trace resume is
capped at 4 MiB per session file. Archive or remove an oversized/damaged raw file
to retry from bounded PostgreSQL history; valid raw resume does not duplicate the
PostgreSQL seed.

The SDK retains append errors instead of panicking when tmpfs fills. A capacity
monitor emits an audit error, and new sessions skip tracing when capacity is nearly
exhausted. The model run and PostgreSQL history remain available. Oversized SDK
tool-result spills in the API are disabled; bounded head/tail text and isolated
worker artifacts replace that unbounded local write path.

## Validation

Run focused storage, evidence, traffic, workspace and archive tests, including
race checks. The disposable PostgreSQL suite verifies transaction/binding and
restore behavior. Docker integration verifies the actual transcript tmpfs
capacity/inode limit and worker lifecycle. Final integrated results belong in
[Validation](VALIDATION.md); this guide does not claim a check passed merely
because the control is configured.

# PostgreSQL disk bounds, log capacity and verified backups

The supported Compose deployment gives PostgreSQL a real, durable filesystem
boundary. It does not treat `pg_database_size()`, `max_wal_size`, a Docker local
volume `size` option or a monitoring alert as an enforced disk quota.

## Physical PostgreSQL boundary

`postgres` stores a fully allocated ext4 image in the **new** `pgdisk` named
volume. The default `ARTEX_PG_DISK_BYTES=8589934592` is 8 GiB. Accepted sizes are
whole MiB from 512 MiB through 1 TiB. The initial zero-write reserves every backing
block before use; ext4 creation disables discard, and startup rejects a sparse,
resized, symlinked, foreign or unidentified image. Filesystem UUID/length and
root-only image permissions are verified on every mount.

PostgreSQL data, indexes, WAL and ordinary database temporary files live under
`/var/lib/postgresql/bounded/data`. The kernel returns `ENOSPC` at the ext4 boundary;
restarting does not enlarge it. The image is root-owned mode0600 in a root-only
parent directory that the PostgreSQL UID cannot open. The upstream image's legacy
`/var/lib/postgresql/data` mount is an inaccessible, bounded 1 MiB tmpfs in Compose.
A nonempty legacy mount causes startup to refuse rather than adopting, changing
permissions on or replacing an old database.

This requires a Linux Docker engine with loop-device and ext4 support, including
the Linux VM used by Colima or Docker Desktop. A missing capability/filesystem
proof stops startup. Rootless engines, restricted managed container platforms and
hosts that do not expose loop-control need a separately engineered external
hard-quota database deployment; there is no unbounded local-volume fallback.

Build the PostgreSQL target and run the read-only preflight before Compose:

```sh
docker build --target postgres -f Dockerfile.source -t artex-local:postgres .
sh scripts/setup-postgres.sh
```

The preflight loads Docker's normal AppArmor profile on a fresh daemon and checks
tools/loop-control without creating a database volume. The fixed root bootstrap
mounts only the validated image and sets loop autoclear. It temporarily has the
mount/device/identity capabilities in Compose, **including `CAP_MAC_OVERRIDE`**,
and starts AppArmor-unconfined. This is trusted administrator-equivalent setup,
not a privilege granted to PostgreSQL or untrusted assessment workers.

On an AppArmor-enabled host the entrypoint requests `docker-default` for its next
exec, then immediately execs `setpriv`. The profile transition therefore happens
while the trusted bootstrap still has `MAC_OVERRIDE`; `setpriv` then irreversibly
drops to PostgreSQL UID 70, clears supplementary groups and all five capability
sets (effective, permitted, inheritable, bounding, ambient), and retains
`NoNewPrivs=1` before initdb, the supervisor or server runs. The guard requires
exactly **`docker-default (enforce)`**. Failure to enter it, retained capabilities,
a wrong UID or missing no-new-privileges stops startup. The supervisor retains
the exclusive image lock and forwards shutdown signals. Inspect the live process
attributes, not only `HostConfig`, which describes the bootstrap envelope.

### Why the exact single AppArmor profile matters

The earlier hosted kernel forced a `docker-default//&unconfined (enforce)` stack
when an unprivileged transition lacked `MAC_OVERRIDE`. A stack can intersect
permissions rather than remove confinement, but accepting that label alone did
not establish a working PostgreSQL boundary: the next hosted attempt reached
`pg_ctl` and failed signal permissions. Moby's default profile binds same-container
signal/ptrace permissions to its expected peer label; this deployment now requires
the single profile instead of widening signal permissions or accepting arbitrary
stack labels. Bare unconfined, complain/mixed modes, extra stack members and
namespace/prefix variations remain refused. See the
[kernel transition implementation](https://github.com/torvalds/linux/commit/08c2f7c8d4b1434cfae006f3daf4d1bce330b57b),
[Moby profile peer rules](https://github.com/moby/profiles/blob/main/apparmor/template.go)
and [AppArmor stacking semantics](https://www.apparmor.net/advanced/AppArmorStacking/).

Temporary bootstrap `MAC_OVERRIDE` is an explicit tradeoff for that transition;
it is dropped with every other capability before database work. The design does
not add `MAC_ADMIN`, `CAP_KILL`, a permissive signal rule or a global kernel/sysctl
relaxation. The revised bootstrap/client source passed its fresh local privilege and
backup/integration checks below. Full native hosted boundary/image completion
and current-source OIDC remain pending; older image IDs do not become new scans.

### Automatic readiness, SQL and backup clients

Docker starts healthchecks and later exec processes from the container's configured
credentials, not PID1's reduced credentials. The old root `CMD-SHELL pg_isready`
path could therefore regain the bootstrap capability/profile envelope even while
the server was nonroot. The revised [entrypoint](../postgres/bounded-entrypoint.sh)
now shares `exec_confined` and its runtime guard between `--run-postgres` and
`--run-client`. Compose invokes a fixed `CMD` healthcheck:

```text
/usr/local/bin/artex-postgres-entrypoint --healthcheck
```

`--healthcheck` selects the absolute `pg_isready` binary. `--client` accepts only
known absolute PostgreSQL client paths or `/bin/sh`, and executes them **after**
profile transition, UID/group/capability removal and the shared guard. The trusted
root launcher does not evaluate SQL, parse database responses or run the client's
shell before this drop. [Backup code](../scripts/artex-backup.py) routes schema/
table fingerprints, COPY, dump, readiness and restore through this launcher;
[password recovery](../reset-password.sh) uses it for container SQL too.

For an operator SQL command on a reviewed container, preserve the fixed launcher:

```sh
docker exec -i --user 0:0 YOUR_POSTGRES_CONTAINER \
  /usr/local/bin/artex-postgres-entrypoint --client /usr/local/bin/psql \
  -U artex -d artex -v ON_ERROR_STOP=1
```

The `--user 0:0` here starts only the trusted drop launcher. It does **not** run
`psql` as root. Replacing it with a direct root client or merely `--user postgres`
bypasses the complete profile/capability transition. Docker/VM administrators
remain trusted and can deliberately bypass wrappers; this controls automatic
application/maintenance paths, not an administrator's Docker authority.

The final local client fix is verified: **11 live checks** passed for the actual
Docker healthcheck and an active SQL process with UID/GID 70, all five capability
sets zero, no-new-privileges and the single enforcing profile. The revised source
also passed **22 backup/restore**, **16 disk-full**, **7 migration**, **3 reserve**,
**5 pure AppArmor** and **15 backup unit** checks, plus **869 PostgreSQL/Go cases**
with one optional provider skip and 13 additional packages. The fresh backup report
SHA256 is `90370aaf077e6f6379b1c5213cab5c889931d283aaf34975d503d30a74c49eb7`;
all seven watched sources match. See [validation](VALIDATION.md) for tested
`af9eeb56…`, exact client proof and the still-pending full hosted boundary/images.
Runtime-equivalent rebuilds are not automatically new-index vulnerability scans.

The container root filesystem is read-only; `/tmp` and the socket directory are
bounded tmpfs mounts. PostgreSQL is never given the Docker socket, API artifacts,
private signing-key volume or host filesystem. Docker/VM administrators remain
trusted. The parent VM still needs free space for filesystem metadata, images,
logs and other workloads; an 8 GiB database image is not a total host-disk limit.

## Existing data: explicit offline migration

The former `<project>_pgdata` volume is deliberately absent from the new Compose
volume declarations. It is neither automatically adopted nor removed by the new
project's `down --volumes`. **Do not run the old Compose configuration with
`down --volumes` on data you need.**

1. Record the actual project/source volume name using `docker volume ls` and
   container mount inspection. Keep an independently verified backup.
2. Stop the API, broker and PostgreSQL cleanly; no other container may use the
   source volume. Keep existing `.env`, artifact and private-key volumes.
3. Choose a new bound with sufficient space. Migration requires the source to
   fit below 80% of the target's currently available filesystem space.
4. Run the explicit migration:

```sh
python3 scripts/pgdisk.py \
  --legacy-volume YOUR_PROJECT_pgdata --project YOUR_PROJECT \
  --bytes 8589934592 \
  --confirmation 'MIGRATE STOPPED POSTGRESQL VOLUME'
```

The source is mounted read-only. The target must be a **new**
`<project>_pgdisk` volume. The tool accepts PostgreSQL16 only, refuses symlinks,
special files, active/unclean sources and an existing target, copies bytes and
compares every source regular file before marking migration complete. A durable
pending marker blocks startup after an interrupted migration. Failures preserve
both source and target for inspection; no database volume is deleted implicitly.
Custom tablespaces, linked WAL, another major version or an unclean source need
an explicit logical recovery/migration instead. Do not remove a postmaster PID
file or WAL merely to bypass these checks.

Start the new Compose deployment, verify records/settings/evidence and retain the
old volume until a separate restore exercise succeeds. Size mismatches are
rejected; changing an environment variable never silently expands or shrinks an
existing database. Expansion is an explicit offline migration to a new, larger
bounded volume, preserving the original.

## Disk-full recovery

Disk exhaustion is an availability failure. PostgreSQL may reject writes or panic
and stop if WAL cannot be written. A restart alone cannot promise recovery while
the filesystem remains full. The deployment never deletes database/WAL files or
automatically grows storage to hide the failure.

Each new filesystem contains a **64 MiB root-owned zero reserve**, outside PGDATA.
PostgreSQL cannot consume or unlink it. After stopping producers and the database,
an operator can explicitly release only this generated reserve:

```sh
python3 scripts/pgdisk-maintenance.py --volume YOUR_PROJECT_pgdisk --bytes 8589934592

python3 scripts/pgdisk-maintenance.py --volume YOUR_PROJECT_pgdisk --bytes 8589934592 \
  --release-recovery-reserve \
  --confirmation 'RELEASE GENERATED POSTGRESQL RECOVERY RESERVE' 
```

The maintenance entrypoint verifies the reserve's name, root ownership, mode,
length, link count and known all-zero SHA256 before unlinking it. It changes no
user database files. It is never released or recreated automatically. This small
recovery window can allow restart, a verified export and deliberate application/SQL
reclamation. It is not extra ongoing capacity: investigate growth immediately and
migrate to a larger bounded target if necessary. Never delete `pg_wal`, relation
segments or unknown files to make the database start.

## Docker and host log limits

API, PostgreSQL and broker containers use Docker's `local` driver with
`max-size=10m`, `max-file=3` and compression. Every per-run worker uses `2m × 2`.
These are per-container rotation budgets with record/format overhead, not a shared
30 MiB cap across an unlimited number of containers. Worker lifecycle/admission
bounds and cleanup still matter; remove obsolete containers deliberately.
PostgreSQL writes server logs to stderr rather than an unbounded collector file;
statement logging and error parameter logging are disabled in Compose.

`deploy/docker-daemon.example.json` supplies the same default for other **new**
containers. `deploy/journald-artex.conf` bounds the dedicated Linux VM's journal
at 256 MiB persistent/64 MiB runtime with free-space and seven-day retention
settings. To merge these settings while preserving unrelated daemon options:

```sh
# Run INSIDE the dedicated Linux Docker VM as root.
python3 scripts/configure-docker-logs.py --apply
```

The helper saves private copies of changed configuration, rejects malformed or
ambiguous JSON and symlink targets, and never restarts services or deletes old
logs. Schedule a maintenance window, stop workloads, restart Docker and
systemd-journald, then recreate existing containers and inspect `HostConfig.LogConfig`.
Old containers retain their original logging policy until recreated. On Docker
Desktop, merge the daemon settings through Docker Engine settings; VM/system log
policy is also subject to Desktop's supported controls. Do not overwrite an
existing daemon configuration wholesale or claim templates alone changed a host.

Rotation discards older logs. Send required audit records to a separately managed,
authenticated sink before relying on local rotation; disk limits do not provide
immutable audit retention. Application files written outside stdout/stderr,
external databases, backups and host crash dumps need their own policies.

## Earlier local operational checkpoint — 19 November 2026

On the dedicated `colima-artex-test` VM, Docker and systemd-journald were restarted
and active with the merged policy. A new container inherited compressed Docker
`local` logging at `10m × 3`. A 41,779,200-byte synthetic stdout fixture rotated
to a 3,188,369-byte log directory and retained its final marker. The journal used
16.0 MiB with 256 MiB persistent/64 MiB runtime/seven-day settings. This proves
activation on that VM, not every install or retroactive changes to old containers.

The exact-source encrypted backup/independent bounded-restore integration passed
22/22, and the final rebuilt-image repeat passed all 22 again with the same
report hash and all seven watched source hashes matching at that checkpoint.
The later bootstrap/client edits require a new run and fresh watched hashes.
The separate final Compose acceptance run passed 17/17. The physical capacity
suites passed 16 disk-full, seven migration and three recovery-reserve checks,
plus 15 backup unit, eight cryptographic and four log-policy regressions.
[Validation](VALIDATION.md) records source scope, hashes and remaining worker/image/
hosted checks. These scoped passes do not establish a promised production RTO,
automatic offsite backups, whole-VM disk quotas or universal deletion of copies.

## Signed and encrypted backups

`scripts/artex-backup.py` supports `keygen`, `create` and `verify`. There is
intentionally **no production restore command**. Requirements are Python3,
OpenSSL3+ with CMS AES-GCM/RSA-PSS support, Docker access and the reviewed
PostgreSQL16 image. Run under Linux/macOS or a private WSL filesystem with POSIX
identity permissions. Treat Docker access as administrator access.

Generate separate signing/encryption identities **outside the source checkout and
deployment directory**. Keys are private mode0600 and are never included by the
backup tool. Keep another offline copy of both keys; keep the independently
trusted signing certificate/pin apart from the backup storage. Supply exactly one
PEM certificate; trust bundles and combined identity files are rejected before
archive parsing. Certificates expire
after ten years: monitor expiry, retain historical trust records and verify/rekey
backups deliberately before expiry. Losing the encryption identity loses access;
compromising the signing identity invalidates origin trust.

```sh
python3 scripts/artex-backup.py keygen --directory "$HOME/.artex-backup-identity"

python3 scripts/artex-backup.py create \
  --recipient "$HOME/.artex-backup-identity/encryption-cert.pem" \
  --signing-cert "$HOME/.artex-backup-identity/signing-cert.pem" \
  --signing-key "$HOME/.artex-backup-identity/signing-key.pem" \
  --encryption-key "$HOME/.artex-backup-identity/encryption-key.pem" \
  --output "$HOME/Backups/artex-2026-11-19.cms"

python3 scripts/artex-backup.py verify \
  --backup "$HOME/Backups/artex-2026-11-19.cms" \
  --encryption-key "$HOME/.artex-backup-identity/encryption-key.pem" \
  --trusted-signing-cert "$HOME/.artex-backup-identity/signing-cert.pem"
```

Use `--deployment-directory`, `--project` and repeated `--compose-file` when the
running deployment differs from the default checkout. The default maximum is
32 GiB; `--max-bytes` is an explicit reviewed byte limit, not unlimited copying.
The encrypted input must be a regular file; FIFOs/devices, oversized chunks and
actual input growth beyond the reviewed byte budget are rejected. Row-content
verification has active 600-second deadlines plus database statement/lock timeouts,
and streams directly into a hash rather than buffering the table.
Backups are written outside the source checkout, never overwrite an existing
file and are published only after complete verification. Keep ample private
staging capacity: dump, state copies, tar, decrypted verification files and a
separate preallocated database volume can coexist. Use an encrypted host disk;
cleanup is logical unlinking, not secure erasure of host snapshots.

Creation discovers the running Compose API/database and exact named
artifact/private/broker-state volumes, stops the previously running API/broker containers and
captures a consistent single-operator window. PostgreSQL remains running for a
transactionally consistent custom `pg_dump`. The artifact/private/broker-state volumes are
read-only to the snapshot helper. The stopped broker ledger is preserved along
with its token/policy. Selected `.env`, worker credentials/policy, Compose files
and resolved running-container environments/image identities are included;
ephemeral SDK debug tmpfs contents, external skills,
Docker images, external provider state and arbitrary host files are excluded.
Database credentials/settings, JWT key, finding evidence, holds, workspace files,
archives and their exact byte/hash inventory are covered. An explicit task archive
may already contain a debug trace copy; that admitted artifact remains included.

All other database/filesystem writers must also be quiesced. This is a
single-operator backup, not a distributed atomic snapshot across outside writers
or external providers. Stop/backup failure always attempts to restart exactly the
API/broker containers that were running beforehand; it does not start a previously
stopped service or recreate a changed configuration. Take the outage into account
and preserve unsynchronized worker artifacts before maintenance.

Binary snapshot helpers and the disposable restore database disable Docker log
capture so private tar bytes or failed COPY context cannot enter container logs.
The production service containers retain their bounded operational log policy.

A detached CMS SHA256/RSA-PSS signature covers the complete tar. A fixed versioned
framing of that signature and tar is then CMS-encrypted using AES256-GCM and
RSA-OAEP-SHA256. Independent CMS messages encrypt bounded 8 MiB chunks with
authenticated sequence/final fields; lengths/counts are checked before allocation.
This avoids OpenSSL ASN.1 decoding retaining a multi-gigabyte encrypted OCTET
STRING in memory. The decoder accepts only canonical definite-length DER
AuthEnvelopedData with one RSA-OAEP recipient (SHA256, MGF1-SHA256, empty label),
AES256-GCM with a 12-byte nonce/16-byte tag, and no optional CMS attributes.
CBC, PKCS1v1.5, SHA1 and alternative algorithm profiles are rejected before the
private-key operation. A final full-tar signature check still rejects reordered,
truncated, spliced or attacker-encrypted content before archive parsing.
Encryption alone cannot establish origin because anyone with the
public recipient certificate can encrypt a forged backup. Verification therefore
finishes decryption/GCM authentication, bounds the framing and verifies the
signature against **only** the independently supplied certificate before parsing
any archive. Embedded certificate substitution and system trust-store fallback
are disabled. Links, special files, traversal, duplicates, absent scope members,
size/count excess and file-hash mismatches are refused.

Every successful `create` decrypts/authenticates its actual output, restores its
dump into a **new networkless bounded PostgreSQL container/volume**, and compares
normalized schema plus sorted canonical JSON row counts/SHA256 for every user
table. It also verifies the full artifact/private/config inventory, including the
JWT key and deployment environment. Verification removes only its own disposable
resources. It never connects its restore to production or overwrites production
state. Keep the reported backup ID, encrypted-file hash and verification date in a
separate operational record; this exercise does not replace a complete human-run
recovery of application keys, configuration and UI workflows.

## Operational recovery objectives and retention

No automatic backup schedule or offsite transfer is silently enabled. Choose an
RPO: for example, a daily verified backup has up to 24 hours of committed-data loss
plus any failed/missed runs. Schedule through your existing trusted scheduler,
monitor failures and keep at least one independently accessible copy. Do not
claim point-in-time recovery without a separate tested WAL archive/replica design.

RTO depends on download/decryption time, preallocation, logical restore, file copy
and operational configuration. Measure it on realistic data and a separate
recovery host; the disposable verification proves integrity/content, not a
promised production recovery duration. A full restoration also needs the exact
private keys/config, reviewed images, worker policies and target authorization.

The tool never deletes existing backups automatically. Set an explicit encrypted
backup retention/storage policy, honor assessment/legal holds and monitor the
backup destination capacity. Raw capture's 30-day policy applies to the managed
recorder, not historical encrypted backups. Backup deletion cannot erase copies
held by an offsite provider, host snapshot or offline identity holder.

## Reproducible validation

```sh
python3 scripts/test-log-policy.py
python3 scripts/test-backup.py
python3 -I scripts/test-backup-crypto.py
python3 scripts/test-pgdisk.py
python3 scripts/test-pgdisk-migration.py
python3 scripts/test-backup-integration.py
sh scripts/test-isolated.sh
```

The live filesystem suite uses only a new disposable 512 MiB volume and checks
actual `ENOSPC`, failed database growth, reserve isolation/release, unchanged
backing length and committed data after recovery/restart. Crypto regressions
exercise corruption, wrong keys, public-recipient forgery, signer substitution,
file manifest mismatch and unsafe archives. The backup integration creates an
independent synthetic Compose project and verifies real dump/restore plus source
availability. See [Validation](VALIDATION.md) for exact completed runs and image
identities; a script's existence is not evidence that a live check passed.

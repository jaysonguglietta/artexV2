# Secure local deployment

ARTEX is a single-operator assessment application. Use it only on systems you own
or have permission to test. Deploy it in a dedicated assessment VM with a dedicated
Docker daemon; do not use shared multi-tenant hosting.

## Execution boundary

Follow [Isolated task execution](WORKER-ISOLATION.md) to build the task image,
configure approved destination CIDRs, and start the private worker broker. The API
never runs model-requested shell/filesystem/custom/MCP tools locally. Missing
worker configuration blocks execution. The legacy ARTEX_ALLOW_HOST_EXECUTION=1
flag is rejected in production. Prompt review and coverage scope do not authorize
network access; the broker's operator-owned policy controls worker egress.

The API keeps database/model credentials and signing keys. Workers receive none
of those credentials. Only the trusted broker gets the Docker socket. Do not mount
it into the API or worker, publish the broker port, or relax its network isolation.

## Chat-only startup

```sh
cp .env.example .env
# Set unique POSTGRES_PASSWORD and ARTEX_ADMIN_PASSWORD values in .env.
./scripts/setup-workers.sh --secret-only
docker compose build postgres
sh scripts/setup-postgres.sh
docker compose up --build -d
```

Build the PostgreSQL image before its preflight; the preflight primes normal
confinement on a fresh daemon without creating or touching database volumes.
The new bounded `pgdisk` volume never automatically adopts the former `pgdata`
volume. Preserve existing data and follow the explicit offline migration in
[PostgreSQL capacity and backup operations](POSTGRES-BACKUP.md); do not remove
legacy volumes or use the old configuration with `down --volumes`.

Open http://localhost:8787. The API publishes only on loopback; the database and
recording proxy are not published. Chat, records and the UI are available without
a worker, but autonomous tools require the worker deployment. Configuration and
secrets are private files excluded from Git and build contexts. Keep backups of
PostgreSQL and the private signing-key volume; protect and rotate them after any
compromise. Do not upload runtime evidence or configuration to GitHub.

## PostgreSQL startup and automatic clients

The bounded database requires a trusted root mount bootstrap. The revised source
adds temporary `MAC_OVERRIDE` only to enter the single enforcing `docker-default`
profile at first exec into `setpriv`; the database supervisor/server must then be
UID 70 with all five capability sets empty and no-new-privileges set. This is an
explicit bootstrap privilege tradeoff, not ongoing database authority. No
`MAC_ADMIN`, `CAP_KILL` or global AppArmor/sysctl relaxation is used.

Docker healthchecks and `docker exec` start with container-configured credentials;
they do not inherit PID1's later privilege drop. The source now routes automatic readiness/SQL/dump/restore clients through the
same fixed entrypoint `--healthcheck`/`--client` launcher and common guard, with
only known absolute clients or `/bin/sh` executed after the drop.
Do not replace that boundary with a direct root `pg_isready`, `psql`, `pg_dump` or
`pg_restore` invocation. Docker/VM administrators remain trusted and can bypass
in-container wrappers manually. The revised client/bootstrap passed 11 actual
process/healthcheck checks and a fresh 22-check encrypted restore locally, together
with the database/Go suites. [Validation](VALIDATION.md) separates those current
passes from the earlier startup failures and pending hosted boundary/image/OIDC
completion. Follow [PostgreSQL operations](POSTGRES-BACKUP.md)
for the single-profile requirement and backup limits.

## Sessions and remote access

For remote UI access, use a private authenticated tunnel or HTTPS reverse proxy
with a dedicated hostname/browser profile. Forward the original Host header.
Browser sessions use HttpOnly, SameSite=Strict cookies; Secure is required away
from loopback. Cookies are host-scoped, not TCP-port-scoped: an untrusted service
on another port at that hostname can receive them. Loopback HTTP is only for a
private throwaway environment.

Native clients may use the login response token in an Authorization Bearer header.
Query tokens are rejected. Browser storage contains only a UI marker, never the
JWT. Sessions expire after 12 hours; logout persists a digest revocation and
password changes invalidate older sessions. JWT verification requires HS256, the
expected subject, and expiration. Same-origin mutation checks reject cross-origin
requests. The dev script allows one explicit loopback frontend origin; leave
ARTEX_DEV_ORIGIN unset in production.

## Review, TLS and updates

Model review defaults to deny on failure or malformed output. Previously saved
explicit allow-on-failure policies must be changed manually to deny. Human review
is an additional control, not a sandbox. TLS validation is on; keep assessment-only
insecure target exceptions out of credential/control-plane traffic. MCP workers
always verify TLS. Production in-process update and rollback are permanently
refused, including with the legacy enabling flag. Build reviewed source or
externally verify [signed artifacts](SIGNED-RELEASES.md) before installation;
read-only version checks remain available.

## Verification and publication

Run scripts/test-isolated.sh for disposable PostgreSQL integration tests. Run
frontend type, test, catalog, lint and static-build checks; scripts/audit-npm.py
verifies every npm advisory against live upstream withdrawal status and fails
closed on active or unverifiable alerts. Keep test/build inputs trusted.

Before publication, inspect the clean-copy manifest, retain AGPL and original
attribution, and exclude .env, private keys, runtime data, logs, evidence, dependency
directories and binaries. The worker design reduces tool compromise exposure;
container/kernel hardening and centralized append-only audit remain operational
responsibilities. [Image gates](IMAGE-SECURITY.md), [signed releases](SIGNED-RELEASES.md),
[raw retention](EVIDENCE-STORAGE.md) and [database/log/backup controls](POSTGRES-BACKUP.md)
are implemented with scoped proof in [validation](VALIDATION.md). Follow their
activation/recovery instructions; configured controls are not evidence of a
completed final scan or active host policy.

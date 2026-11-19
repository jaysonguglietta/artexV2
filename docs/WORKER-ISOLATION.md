# Isolated task execution

The API handles authentication, model requests, task records and audit records.
Untrusted shell, filesystem, custom Python/command/HTTP, interactive-shell and MCP
operations run in a separate task container through an authenticated broker. The
API has no Docker socket, and task containers receive no API database DSN, JWT
signing key, broker token, model-provider keys or host filesystem mounts.

## Trust boundaries

The broker requires a dedicated named bridge network and rejects host, container,
none, and default-bridge modes. The broker is a trusted management service with Docker-daemon authority. A broker
compromise is a host compromise. Keep it on the private management network, never
publish its port, and mount its token only into the API and broker. This design
protects against compromised task tools; it does not protect against a compromised
API, broker, Docker daemon, kernel, or operator account. Use a dedicated assessment
VM and Docker daemon. This is a single-operator product, not multi-tenant hosting.

Each agent run gets its own container and bounded 512 MiB tmpfs workspace.
Sibling intents do not share live mounts or processes; small committed task
artifacts are synchronized through the API. Containers have a read-only image,
1 GiB memory, one CPU, 128-process limit, no host networking and no host PID
namespace. The operator-owned task CIDRs apply to every run of that task.

The bootstrap uses NET_ADMIN, SETUID, SETGID and SETPCAP only to install firewall
rules and start the isolated processes. The tool daemon and fresh RPC clients run
as UID/GID 10001, with no-new-privileges and every capability set empty. A separate
root-identity PID1 watchdog also has no capabilities and accepts no tool requests.
The UID boundary prevents task processes from renewing or modifying its lease.
Only a fixed broker helper can renew it. API sessions renew every 20 seconds;
without a renewal the watchdog exits within 90 seconds and the kernel terminates
all descendants, including detached/double-forked processes. Normal cancellation
uses a separate authenticated close channel and removes that run's entire cgroup
without waiting for a foreground tool or stopping sibling runs.

IPv6 outbound traffic is denied except loopback. Workers cannot change their
firewall, regain privileges, access the Docker socket or mount host paths. The
API and broker drop all capabilities. The broker refuses host/shared/default
network modes, inspects the dedicated bridge driver, disables image healthchecks
and fixes the worker entrypoint. Do not weaken those deployment settings.

## Build and start

Complete [installation prerequisites and private configuration](INSTALLATION.md)
first. Preserve the resulting `.env`; do not copy the example over configured
credentials. Use the repository's pinned Dockerfile and lockfiles:

```sh
# .env must already contain your private credentials from the installation guide.
docker build -f Dockerfile.source --target worker -t artex-local:worker .
docker build -f Dockerfile.source --target broker -t artex-local:broker .
sh scripts/setup-workers.sh
docker compose build postgres
sh scripts/setup-postgres.sh
# Review .secrets/worker-policy.json and .env.workers before starting.
docker compose --env-file .env --env-file .env.workers \
  -f docker-compose.yml -f compose.workers.yml up --build -d
```

The setup script creates a random private broker token if none exists and records
the worker's immutable local image ID. It creates the dedicated local bridge
`artex-isolated-workers`, labelled `artex.purpose=isolated-worker-network`, and
refuses an existing network with a different owner label, driver, scope or
internal-network setting. The bridge is managed by setup, not Compose: it remains
after `docker compose down`. Never attach the API or broker to this bridge. To
remove it, first stop the API, broker and all worker containers, inspect that it is
unused, then remove the network deliberately. Do not adopt a shared network to
work around a setup refusal.

Setup never changes an existing token. The `.secrets` directory is owner-only;
its mounted token file is readable by container
UID 10001. Neither the token nor operator policy enters a build context or upload
copy. Setup records the Docker socket's numeric group ID in `.env.workers`;
Compose adds that supplemental group to broker UID 10001.

Create a task in the UI, note its numeric ID, and add a narrowly approved range to
`.secrets/worker-policy.json`, for example:

```json
{
  "sessions": {
    "task-42": ["198.18.0.10/32"]
  },
  "dns_servers": ["1.1.1.1"],
  "management_cidrs": ["192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12"]
}
```

The benchmark address above is an example, not a public test target. Replace it
with authorized target CIDRs. The restrictive example denies private management
networks; private target assessments require a reviewed, non-overlapping set of
actual management/host deny ranges. Deny ranges always win. Include every host,
control-plane endpoint, management network, cloud metadata range, and other
sensitive destination. The broker also denies all Docker subnets, the worker's
Docker gateway, link-local/metadata, multicast and reserved destinations. Start all
management networks before workers; restart the broker after changing networks.

Restart `worker-broker` to load a changed policy. Missing policy, authentication,
startup, broker connectivity, or firewall setup fails closed. There is no host
execution fallback. ARTEX_ALLOW_HOST_EXECUTION=1 is rejected by the production
entrypoint. Existing local execution fixtures work only inside Go test binaries.

Non-task chats use `session-<workspace hash>` scopes and custom-tool editor tests
use an isolated tool-tests workspace. A refusal identifies the exact scope to
configure. MCP discovery without a task uses the explicit `discovery` scope; allow
only the required integration destinations there. Do not grant broad discovery
ranges just to make a connectivity warning disappear.

## Network and integration behavior

The task's allowlist is an operator-owned broker file, not model-editable task
coverage scope. Docker is configured to forward DNS only to the explicit IPv4 resolvers.
Its embedded resolver and the read-only resolv.conf are part of the trusted Docker boundary. DNS requests are network capabilities too:
use an assessment resolver when queries must remain private. TLS verification is
mandatory for MCP. Per-task network enforcement applies to DNS-resolved addresses,
redirects, child processes, custom tools and browser traffic alike.

Configure MCP commands for binaries installed in the worker image. Avoid runtime
package downloads; install reviewed, locked tools in a rebuilt image. Explicitly
configured MCP credentials are sent only to that task's integration client; they
are capabilities granted to that task, not control-plane credentials. Use separate
least-privilege integration accounts. Raw-packet scans requiring root/CAP_NET_RAW
are unsupported; use unprivileged TCP-connect scans. The API recording proxy is
not reachable from workers: custom HTTP tools must use direct target requests.
Captured command output and synchronized files remain available; a distributed
traffic-recording/import pipeline is not implemented.

## Files, lifetime and limits

Worker files are rooted in `/workspace`. The bridge maps the trusted task workspace
prefix to that path, and synchronizes regular files through bounded messages.
Symlinks are never exported. Both ends use os.Root to prevent traversal during
artifact synchronization. Up to 1,000 files and 4 MiB total are synchronized per
call. Each relative path is limited to 1,024 UTF-8 bytes and 32 components including
the filename; synchronization permits at most 2,048 parent directories, including
empty directories encountered during a workspace walk. Returned path maps are
validated before any API filesystem changes. Larger worker files require the
worker Read tool. Large uploads beyond that budget require a separate streamed
transfer mechanism, not a raised HTTP body
limit. Missing/oversized input synchronization returns an error.

The broker admits two bounded data requests and retains at most eight active
run containers. Foreground RPCs are serialized; background scans can run in sibling
containers. Close and lease channels have independent small-body admission so a
long scan cannot hold cancellation behind its data request. Every production run
has a trusted random identity assigned before tool assembly. A private locked,
append-only broker-state ledger prevents admitted IDs from reopening after close
or restart; corruption or its 100,000-record capacity blocks new admission.
Each deployment needs its own token, state volume and dedicated Docker daemon.

API session completion/cancellation closes its run. If control-plane connectivity
is unavailable, the independent watchdog bounds orphan lifetime by its last lease;
this is not an immediate-stop promise during an outage. Broker startup reaps its
labelled orphan containers. Existing admitted IDs remain closed after restart.
For ledger maintenance, stop both API and broker, preserve needed data, and rotate
the broker token/reset private state deliberately. Never silently evict replay
records or reuse a token/state directory across independent deployments.

Task archive/deletion requests collect bounded regular-file snapshots from the
actual tmpfs, then pause and retire matching run containers. Read/write limits or
conflicting same-path sibling files abort preservation explicitly. Snapshots are
best-effort copies of untrusted task evidence, not atomic forensic acquisitions:
unmanaged hostile code can write while files are read. A long pause can expire
the watchdog on resume. Unsynchronized large files and interactive state are
ephemeral; export needed evidence before closing a run. Explicit file deletion
may discard it. Broker restarts are not a substitute for an evidence backup.

## Validation

Run `go test ./isolation ./guard`, the normal integration suite, and the synthetic
Docker-VM test `python3 scripts/test-workers.py` after building both worker and
broker images. The synthetic test temporarily assigns benchmark-only addresses
to the dedicated Docker VM's loopback and cleans them up. Never run this fixture
against a shared or production Docker daemon. It verifies allowed and denied
adjacent destinations, denied Docker-host access, capability dropping, absent
service credentials/socket mounts, cross-task files, and authenticated policy
admission. Passing these checks does not prove kernel/container escape resistance.

`python3 scripts/test-setup-workers.py` checks setup's labelled bridge creation,
safe reuse, conflict rejection, and secret-only mode using a fake Docker executable.
It contacts no Docker daemon.

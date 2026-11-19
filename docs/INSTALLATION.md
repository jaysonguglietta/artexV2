# Installation and first run

This guide installs the source checkout for one operator. The Go API serves the
bundled web interface; PostgreSQL stores records. Scanning additionally needs a
trusted broker and isolated worker containers. UI-only installation does **not**
enable scanning. No model key is required merely to open the interface.

Use a dedicated assessment VM/Docker daemon. The broker has daemon authority;
never give it a shared production Docker daemon. The interface binds only to
`127.0.0.1:8787`. Do not publish PostgreSQL, the broker, or the recording proxy.

## 1. Prerequisites and Mac setup

You need Git (or the downloaded source ZIP), Python 3, Docker Engine and the
Docker Compose plugin. Native Go/Node installations are unnecessary for the
container build. Network access is required to fetch pinned images, packages and
modules. Initial builds can take several minutes or longer.

The following Apple Silicon Mac path uses Homebrew and a dedicated Colima Linux
VM. CLI/plugin setup follows the [official Colima installation guide](https://colima.run/docs/installation/). If Homebrew is absent, install it using the official instructions at
[brew.sh](https://brew.sh/). Then run in Terminal:

```sh
brew install colima docker docker-compose docker-buildx python
mkdir -p "$HOME/.docker/cli-plugins"
ln -sfn "$(brew --prefix)/opt/docker-compose/bin/docker-compose" "$HOME/.docker/cli-plugins/docker-compose"
ln -sfn "$(brew --prefix)/opt/docker-buildx/bin/docker-buildx" "$HOME/.docker/cli-plugins/docker-buildx"
colima start --profile artex --cpus 4 --memory 8 --disk 64
export DOCKER_CONTEXT=colima-artex
```

These VM allocations are practical starting values, not measured minimums. Keep
adequate Mac disk space: a virtual disk size is a capacity ceiling, not free host
storage. Worker images, build caches, an 8 GiB database filesystem, and evidence
all consume storage. A full installation can exceed 32 GiB. Reduce CPU/memory
only if your Mac needs it; worker concurrency competes with the API and database.
Apple Silicon uses native arm64 images; Intel requires amd64. Only the Apple
Silicon Colima path was exercised for this installation review.

If an existing dedicated ARTEX profile is named `artex-test`, reuse it with
`colima start --profile artex-test` and `export DOCKER_CONTEXT=colima-artex-test`
instead. Do not create a new daemon expecting it to contain the old daemon's data.

Check before continuing:

```sh
python3 --version
docker version
docker compose version
docker buildx version
docker info
df -h .
```

`docker version` must show a working **Server**, not only a client. Keep
`DOCKER_CONTEXT` set in each Terminal session running the commands below; this
also routes the repository's setup scripts to the intended daemon.

For a dedicated Linux VM, install Engine and the Compose/Buildx plugins using
[Docker's Engine instructions](https://docs.docker.com/engine/install/), then
perform the same checks. The bounded database requires Linux loop devices and
AppArmor confinement. Docker Desktop and other VM backends are not verified by
this guide: run the database preflight and do not bypass it if unsupported.

## 2. Get the source

```sh
git clone git@github.com:jaysonguglietta/artexV2.git
cd artexV2
```

SSH cloning requires a GitHub SSH key with repository access. If SSH is not set
up, use HTTPS (private repositories still require authentication):

```sh
git clone https://github.com/jaysonguglietta/artexV2.git
cd artexV2
```

Alternatively, unzip the downloaded `artexV2-main.zip`, then `cd` into the extracted
`artexV2-main` directory. Every remaining command runs from the source root
containing `docker-compose.yml`. ZIP downloads may lose executable permissions;
this guide invokes setup scripts with `sh` explicitly.

A source checkout is not a signed release. For verified release installation,
read [Signed releases](SIGNED-RELEASES.md); do not assume a versioned release exists.

## 3. Create private configuration

On a **fresh** checkout only:

```sh
umask 077
cp -n .env.example .env
python3 - <<'PY'
from pathlib import Path
import secrets
p = Path('.env')
s = p.read_text()
for name in ('POSTGRES_PASSWORD', 'ARTEX_ADMIN_PASSWORD'):
    if f'{name}=\n' not in s:
        raise SystemExit(f'{name} already set or missing; preserve existing configuration')
for name in ('POSTGRES_PASSWORD', 'ARTEX_ADMIN_PASSWORD'):
    s = s.replace(f'{name}=\n', f'{name}={secrets.token_hex(32)}\n', 1)
p.write_text(s)
p.chmod(0o600)
PY
sh scripts/setup-workers.sh --secret-only
```

The generator refuses to overwrite configured passwords. Keep `.env` and
`.secrets` private and out of Git, issue reports and screenshots. The database
password is URL-safe because Compose embeds it in a connection URI. Never change
an initialized database's password just by editing `.env`; PostgreSQL retains its
existing password and the API will lose access. Admin environment credentials
bootstrap a fresh database; later password changes belong in the application.
Leave `ARTEX_SETUP_TOKEN` and `ARTEX_WORKER_URL` empty for this password-based,
UI-only setup. The worker token file is required by base Compose even without a
running broker; `--secret-only` creates it without enabling tools.

## 4. Build and start the UI/database

```sh
docker compose build postgres artex
sh scripts/setup-postgres.sh
docker compose up -d --no-build
docker compose ps
docker compose logs --tail 50 postgres artex
curl --fail http://localhost:8787/api/health
```

Wait for PostgreSQL to become healthy and the API to stay running. On a new daemon,
the preflight must run after building the database image and before startup; it
loads the normal confinement profile without touching database volumes. It is
safe to repeat after a VM restart. Do not replace the database with a privileged
container or remove confinement to work around a failure.

Open [http://localhost:8787](http://localhost:8787). Log in with username **ARTEX**
(case-sensitive) and the `ARTEX_ADMIN_PASSWORD` value in your private `.env` file.
Open that file locally to retrieve the password; avoid printing it in shared logs.
The interface and health endpoint should return HTTP 200. Successful login alone
does not prove model access or scanning works.

Configure a model provider, model and API key in the interface before sending a
chat. Use a key you control; provider billing and outbound connectivity are
separate prerequisites. Save and perform a small chat to verify provider access.
Do not put provider secrets in source files. To run assessment tools, complete
step 5; missing worker configuration deliberately blocks executable tools.

## 5. Enable isolated scanning

Read the full [worker isolation guide](WORKER-ISOLATION.md), including its policy
and trust boundaries. From this same checkout and Docker context:

```sh
docker build -f Dockerfile.source --target worker -t artex-local:worker .
docker build -f Dockerfile.source --target broker -t artex-local:broker .
sh scripts/setup-workers.sh
```

Setup records the worker image ID and Docker socket group in `.env.workers` and
creates a private `.secrets/worker-policy.json`. The example policy is **not**
authorization for any real network. Create a task without starting execution,
note its numeric ID, and configure only your authorized destination CIDRs under
`task-<ID>`. Retain management/host deny rules; deny rules override allow rules.
See the worker guide for private target networks, DNS and non-task scopes. Do not
substitute `0.0.0.0/0` to make an error disappear.

Start the full stack using both environment and Compose files:

```sh
docker compose --env-file .env --env-file .env.workers \
  -f docker-compose.yml -f compose.workers.yml up --build -d
docker compose --env-file .env --env-file .env.workers \
  -f docker-compose.yml -f compose.workers.yml ps
```

These commands assume step 4 has already built and preflighted PostgreSQL. The
override supplies the private broker URL; do not expose port 8790. Restart the
broker after editing its policy:

```sh
docker compose --env-file .env --env-file .env.workers \
  -f docker-compose.yml -f compose.workers.yml restart worker-broker
```

Verify with a small assessment against an explicitly authorized test target, then
check task output. A denied destination must remain denied. The UI/database path
was verified on Mac for this review; a fresh full worker build and scan were not
repeated. Existing worker validation evidence is described in
[Validation](VALIDATION.md), not implied by this guide.

## 6. Stop, restart, back up and update

For UI-only installations:

```sh
docker compose stop
# Later, after starting the same VM and exporting DOCKER_CONTEXT:
sh scripts/setup-postgres.sh
docker compose up -d --no-build
```

For worker-enabled installations, use the **same two environment files and two
Compose files from step 5** for `stop`, `up`, `ps` and `logs`. Re-run
`sh scripts/setup-workers.sh` after rebuilding the worker to refresh its pinned
image ID, then recreate the broker using the full startup command. Do not rotate
its token or delete state casually: these enforce replay protection.

Keep the checkout location, Docker context and Compose project identity stable.
If you use `-p artex-mac` (as in the assisted Mac installation), use it on **every**
Compose command. Changing any of these can select different named volumes and
appear to lose data. `stop` preserves volumes. **Never use `down --volumes` or
volume pruning unless deliberately destroying the installation's data.** Stopping
the VM also interrupts every other workload on that VM.

Before updates, back up PostgreSQL, signing keys and required evidence using
[PostgreSQL backups and recovery](POSTGRES-BACKUP.md) and
[evidence storage](EVIDENCE-STORAGE.md). Do not copy a live database directory as
an assumed valid backup. Review changes, update a clean Git checkout with
`git pull --ff-only`, rebuild images, repeat preflight and recreate with the same
Compose invocation. ZIP installations should use a separately reviewed source
copy while explicitly preserving private configuration and project identity.
In-process production update/rollback is disabled. Existing legacy `pgdata`
volumes need the documented offline migration; they are never auto-adopted.

## Troubleshooting

| Symptom | Check and recovery |
|---|---|
| Cannot connect to Docker / missing socket | Start the intended Colima profile; export its `DOCKER_CONTEXT`; run `docker info`. Do not switch daemons to hide the error. |
| `docker: unknown command: compose` / missing Buildx | Repeat CLI-plugin setup in step 1 and check plugin versions. |
| SSH `Permission denied (publickey)` | Configure a GitHub SSH key or use HTTPS/ZIP with repository access. |
| `permission denied` running a setup script | Invoke it with `sh scripts/<name>.sh`; ZIP extraction may discard executable bits. |
| `no space left on device` / repeated API restarts | Inspect `docker compose logs --tail 50 artex`, Mac `df -h .`, `docker system df`, and `colima ssh --profile artex -- df -h /var/lib/docker`. Expand the dedicated VM as shown below; do not delete volumes. |
| PostgreSQL preflight / loop-control / AppArmor failure | Confirm the supported dedicated Linux VM and correct image/context. Keep the failure output; do not disable confinement. |
| `localhost:8787` refuses connections | Check `docker compose ps` and logs. Confirm PostgreSQL is healthy and API is not restarting. If both run, inspect VM port forwarding. |
| Port 8787 already in use | Identify the owning local process/other ARTEX deployment and stop it deliberately, or review a loopback-only port override. Do not expose the service on `0.0.0.0`. |
| Login fails | Use username `ARTEX`, confirm this is the expected context/project/database, and use the current application password. Editing the bootstrap environment does not reset existing accounts. |
| Model request fails | Check saved provider/model/key, provider account access and connectivity. Share sanitized errors only. |
| Tools blocked / broker unavailable | Use the complete worker Compose invocation, inspect broker logs and the exact reported policy scope, and restart after policy changes. No host-execution fallback exists. |
| Records seem missing after restart | Compare Docker context, Compose project name and volumes with the original setup. Preserve all volumes while investigating. |

To expand the example VM without deleting data (choose a larger capacity your Mac
can support; this interrupts its workloads):

```sh
colima stop --profile artex
colima start --profile artex --disk 80
export DOCKER_CONTEXT=colima-artex
sh scripts/setup-postgres.sh
docker compose up -d --no-build
```

Use your actual profile and, if enabled, the full worker startup invocation.
Confirm free space afterward. VM capacity and the bounded PostgreSQL filesystem
are different: resizing the VM does not resize an existing database filesystem.
Follow the database operations guide for database capacity changes.

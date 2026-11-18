#!/bin/sh
# Prepare private local broker credentials without touching an existing secret.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
umask 077
mkdir -p .secrets
if [ ! -f .secrets/worker-token ]; then
 python3 -c 'import secrets; print(secrets.token_hex(32))' > .secrets/worker-token
fi
# Docker Compose file secrets must be readable by container UID 10001. The
# enclosing directory remains private to the operator; no secret is in an image.
chmod 700 .secrets
chmod 644 .secrets/worker-token
if [ "${1:-}" = --secret-only ]; then exit 0; fi
[ -f .secrets/worker-policy.json ] || cp worker/policy.example.json .secrets/worker-policy.json
chmod 644 .secrets/worker-policy.json
docker image inspect artex-local:broker >/dev/null 2>&1 || {
 echo 'Build the broker first: docker build --target broker -f Dockerfile.source -t artex-local:broker .' >&2
 exit 1
}
worker_image=$(docker image inspect artex-local:worker --format '{{.Id}}')
socket_gid=$(docker run --rm -v /var/run/docker.sock:/var/run/docker.sock --user 0:0 --entrypoint stat artex-local:broker -c '%g' /var/run/docker.sock)
case "$socket_gid" in ''|*[!0-9]*) echo 'Cannot determine Docker socket group' >&2; exit 1;; esac
worker_network=artex-isolated-workers
if ! network_json=$(docker network inspect "$worker_network" --format '{{json .}}' 2>/dev/null); then
 docker network create --driver bridge --label artex.purpose=isolated-worker-network "$worker_network" >/dev/null || {
  echo 'Cannot create the dedicated worker bridge; inspect the Docker daemon and rerun setup.' >&2
  exit 1
 }
 network_json=$(docker network inspect "$worker_network" --format '{{json .}}')
fi
printf '%s\n' "$network_json" | python3 -c '
import json, sys
try:
 network = json.load(sys.stdin)
 valid = (
  network.get("Name") == "artex-isolated-workers"
  and network.get("Driver") == "bridge"
  and network.get("Scope") == "local"
  and network.get("Internal") is False
  and (network.get("Labels") or {}).get("artex.purpose") == "isolated-worker-network"
 )
except (ValueError, AttributeError, TypeError):
 valid = False
if not valid:
 sys.exit("Refusing artex-isolated-workers: expected a setup-owned, local, non-internal bridge labelled artex.purpose=isolated-worker-network. Stop the API, broker and active workers, inspect the conflicting network, and remove it manually only if unused before rerunning setup. Never adopt a shared or management network.")
'
printf 'ARTEX_WORKER_IMAGE=%s\nARTEX_DOCKER_GID=%s\n' "$worker_image" "$socket_gid" > .env.workers
printf '%s\n' 'The dedicated worker bridge is retained outside Docker Compose; compose down will not remove it.'
printf '%s\n' 'Review .secrets/worker-policy.json: add exact task-<ID> target CIDRs and keep every management/host network denied.'
printf '%s\n' 'Start with: docker compose --env-file .env --env-file .env.workers -f docker-compose.yml -f compose.workers.yml up --build -d'

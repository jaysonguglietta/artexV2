#!/bin/sh
# Build reviewed local source; never download and execute an installer script.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
command -v docker >/dev/null 2>&1 || { echo 'Install a trusted Docker engine first.' >&2; exit 1; }
docker compose version >/dev/null
if [ ! -f .env ]; then
 umask 077
 cp .env.example .env
 printf '%s\n' 'Created private .env. Set POSTGRES_PASSWORD and ARTEX_ADMIN_PASSWORD, then rerun.'
 exit 0
fi
./scripts/setup-workers.sh --secret-only
docker compose up --build -d
printf '%s\n' 'Open http://localhost:8787. Chat-only mode is active until isolated workers are configured.'
printf '%s\n' 'For scanning, follow docs/WORKER-ISOLATION.md. Docker is not installed or reconfigured by this script.'

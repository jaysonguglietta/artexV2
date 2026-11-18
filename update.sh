#!/bin/sh
# Rebuild the current reviewed checkout. No implicit pull or upstream image trust.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
case "${1:-}" in
 --local) exec ./build.sh ;;
 --workers)
  docker build --target worker -f Dockerfile.source -t artex-local:worker .
  docker build --target broker -f Dockerfile.source -t artex-local:broker .
  ./scripts/setup-workers.sh
  exec docker compose --env-file .env --env-file .env.workers -f docker-compose.yml -f compose.workers.yml up --build -d ;;
 '') exec docker compose up --build -d ;;
 *) printf '%s\n' 'Usage: ./update.sh [--local|--workers]' >&2; exit 1 ;;
esac

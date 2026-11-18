#!/usr/bin/env bash
# Run the loopback backend and Next.js development server together.
# /api requests are proxied to the backend. Use the embedded production build
# described in README.md for deployment; development servers are local tools.
set -euo pipefail
cd "$(dirname "$0")"
backend_pid=
frontend_pid=
cleanup() {
  trap - EXIT INT TERM
  for child_pid in "$backend_pid" "$frontend_pid"; do
    if [[ -n "$child_pid" ]]; then kill "$child_pid" 2>/dev/null || true; fi
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Build first so the tracked process is the backend, not a go-run wrapper.
binary_dir=$(mktemp -d "${TMPDIR:-/tmp}/artex-dev.XXXXXX")
trap 'cleanup; rm -rf "$binary_dir"' EXIT
go build -o "$binary_dir/artex" ./cmd/artex
ARTEX_DEV_ORIGIN=http://127.0.0.1:3000 "$binary_dir/artex" -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788 &
backend_pid=$!
( cd web && exec node node_modules/next/dist/bin/next dev --hostname 127.0.0.1 --port 3000 ) &
frontend_pid=$!
echo "[dev] Backend: 127.0.0.1:8787; proxy: 127.0.0.1:8788; frontend: http://127.0.0.1:3000 (Ctrl-C to stop)"
wait "$backend_pid" "$frontend_pid"

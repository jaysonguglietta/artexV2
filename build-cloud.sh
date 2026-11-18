#!/usr/bin/env bash
# Build reviewed local source; deployment access policy is configured separately.
set -euo pipefail
cd "$(dirname "$0")"
echo "[*] Building frontend..."
(cd web && npm ci --ignore-scripts && npm run build:static)
echo "[*] Copying static assets..."
rm -rf server/webui/dist
mkdir -p server/webui/dist
cp -R web/out/. server/webui/dist/
echo "[*] Compiling embedded Go backend..."
CGO_ENABLED=0 go build -trimpath -tags embedui -o artex ./cmd/artex
echo "[+] Build completed."

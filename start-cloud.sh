#!/usr/bin/env bash
# Loopback by default. A hosted ingress may require explicit ARTEX_LISTEN_HOST=0.0.0.0;
# use private access controls and HTTPS before making that choice.
set -euo pipefail
cd "$(dirname "$0")"
: "${PORT:?Set PORT to the hosting service's assigned TCP port}"
if [[ ! "$PORT" =~ ^[0-9]{1,5}$ ]] || (( 10#$PORT < 1 || 10#$PORT > 65535 )); then
  echo "PORT must be an integer from 1 to 65535" >&2
  exit 1
fi
artex_listen_host=${ARTEX_LISTEN_HOST:-127.0.0.1}
case "$artex_listen_host" in
  127.0.0.1|0.0.0.0) ;;
  *) echo "ARTEX_LISTEN_HOST must be 127.0.0.1 or 0.0.0.0; use the CLI for other addresses" >&2; exit 1 ;;
esac
echo "[*] Starting ARTEX on $artex_listen_host:$PORT"
exec ./artex -addr "$artex_listen_host:$PORT" -proxy 127.0.0.1:8788

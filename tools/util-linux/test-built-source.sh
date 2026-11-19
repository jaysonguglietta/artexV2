#!/bin/sh
# Mandatory offline checks after package assembly; never installed in the worker.
set -eu
export LC_ALL=C.UTF-8
output=${1:?package output directory is required}
source=$(cat "$output/build-source.path")
case "$source" in /tmp/artex-util-linux.*/util-linux-2.42.4) ;; *) exit 1 ;; esac
work=${source%/*}
test -f "$source/config.h" && test -x "$source/setpriv"
cd "$source"
chmod 0711 "$work"
python3 /opt/artex-util-linux/test-source-security.py
# Ubuntu's default --nonroot suite intentionally skips a root package build.
# Run its security-relevant groups explicitly as the actual unprivileged UID.
chown -R 10001:10001 "$source"
if ! ./setpriv --reuid=10001 --regid=10001 --clear-groups \
    --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs \
    /bin/bash tests/run.sh --srcdir="$source" --builddir="$source" \
    --nonroot --parallel=2 --show-diff libmount mount nsenter setpriv \
    > "$work/upstream-security.log" 2>&1; then
    tail -n 100 "$work/upstream-security.log" >&2
    exit 1
fi
tail -n 12 "$work/upstream-security.log"
cp "$work/upstream-security.log" "$output/source/"

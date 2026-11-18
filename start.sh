#!/bin/sh
# Foreground supervisor for Linux/macOS. Pass arguments directly to ./artex.
# Exit 0 stops; exit 75 requests immediate restart; other exits back off from
# one second to 60 seconds. Self-update is disabled by default. Downloads,
# checksum verification and staged replacement belong to the Go updater.
set -u

cd "$(dirname "$0")" || exit 1

BIN=./artex
[ -x "$BIN" ] || { echo "[artex] Executable is unavailable: $BIN" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

# Forward termination to the child so a PID-1 supervisor permits graceful shutdown.
forward() {
	stopping=1
	if [ "$child" -ne 0 ]; then
		kill -TERM "$child" 2>/dev/null || true
	fi
}
trap forward INT TERM

delay=1
while :; do
	"$BIN" "$@" &
	child=$!

	# A signal interrupts wait; wait again for the child's actual exit status.
	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[artex] Stopped"
		exit 0
	fi

	case "$code" in
		0)
			echo "[artex] Normal exit"
			exit 0
			;;
		"$RESTART_CODE")
			# The next startup completes a deliberately staged update/rollback.
			echo "[artex] Restart requested (applying staged version)..."
			delay=1
			;;
		*)
			echo "[artex] Abnormal exit (code=$code), restarting in ${delay}s" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done

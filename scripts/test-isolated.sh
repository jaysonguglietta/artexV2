#!/bin/sh
# Run trusted integration tests against disposable databases, never a real DSN.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
container="artex-tests-$(date +%s)-$$"
log_dir=$(mktemp -d "${TMPDIR:-/tmp}/artex-tests.XXXXXX")
volume="$container-pgdisk"
volume_created=0
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  if [ "$volume_created" = 1 ]; then docker volume rm "$volume" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
# Exercise the same hardened database image deployed by Compose.
docker build --target postgres -f Dockerfile.source -t artex-local:postgres . > "$log_dir/postgres-build.log" 2>&1 || {
  echo "PostgreSQL image build failed; see $log_dir/postgres-build.log" >&2
  exit 1
}
sh scripts/setup-postgres.sh
# Test-only credential and backing volume; never mount an existing user volume.
if docker volume inspect "$volume" >/dev/null 2>&1; then
  echo "Refusing pre-existing test volume: $volume" >&2; exit 1
fi
docker volume create --label artex.purpose=isolated-tests "$volume" >/dev/null
volume_created=1
docker run -d --name "$container" --label artex.purpose=isolated-tests \
  --memory=1g --cpus=1 -e POSTGRES_USER=artex_test \
  -e POSTGRES_PASSWORD=local-test-only -e POSTGRES_DB=postgres \
  -e ARTEX_PG_DISK_BYTES=1073741824 -p 127.0.0.1::5432 \
  --read-only --cap-drop ALL --cap-add SYS_ADMIN --cap-add MKNOD \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETUID --cap-add SETGID --cap-add SETPCAP --cap-add MAC_OVERRIDE \
  --security-opt no-new-privileges:true --security-opt apparmor=unconfined --device /dev/loop-control \
  --device-cgroup-rule 'b 7:* rwm' --pids-limit 256 \
  --log-driver local --log-opt max-size=10m --log-opt max-file=3 \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=128m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m,uid=70,gid=70 \
  --tmpfs /var/lib/postgresql/data:rw,nosuid,nodev,noexec,size=1m,mode=000 \
  --mount "type=volume,src=$volume,dst=/var/lib/artex-pgdisk" \
  artex-local:postgres >/dev/null
startup_failed() {
  docker logs "$container" > "$log_dir/postgres-startup.log" 2>&1 || true
  echo "PostgreSQL failed to start; private synthetic-fixture diagnostics: $log_dir/postgres-startup.log" >&2
  if [ "${GITHUB_ACTIONS:-}" = true ]; then
    # This container always uses the disposable credential above. Keep hosted
    # startup failures reviewable after its runner is destroyed; never dump env.
    tail -n 60 "$log_dir/postgres-startup.log" | sed 's/local-test-only/[REDACTED]/g' >&2
    docker inspect "$container" --format 'exit={{.State.ExitCode}} oom_killed={{.State.OOMKilled}} error={{json .State.Error}}' >&2 || true
  fi
  exit 1
}
attempt=0
until docker exec --user 0:0 "$container" /usr/local/bin/artex-postgres-entrypoint --healthcheck >/dev/null 2>&1; do
  attempt=$((attempt+1)); [ "$attempt" -lt 120 ] || startup_failed
  running=$(docker inspect "$container" --format '{{.State.Running}}' 2>/dev/null || true)
  [ "$running" != false ] || startup_failed
  sleep 1
done
port=$(docker port "$container" 5432/tcp | sed 's/.*://')
failed=0
for package in db agent config evidence llmrec server; do
  docker exec --user 0:0 "$container" /usr/local/bin/artex-postgres-entrypoint --client \
    /bin/sh -ec 'exec /usr/local/bin/createdb -U "$POSTGRES_USER" "$1"' create-test-db "artex_$package"
  echo "Testing $package (isolated database); logs: $log_dir/$package.jsonl"
  if ! ARTEX_ISOLATED_TEST_DATABASE=1 ARTEX_ALLOW_HOST_EXECUTION=1 ARTEX_PG_DSN="postgres://artex_test:local-test-only@127.0.0.1:$port/artex_$package?sslmode=disable" \
    go test "./$package" -count=1 -timeout=6m -json > "$log_dir/$package.jsonl"; then
    failed=1
    echo "FAILED: $package; see $log_dir/$package.jsonl" >&2
  fi
done
# Run remaining packages without a database configuration.
if ! ARTEX_PG_DSN= go test ./cmd/artex ./cmd/tool-worker ./cmd/worker-broker ./guard ./intercept ./isolation ./llmpool ./mcphttp ./notify ./report ./selfupdate ./sidequestion ./storage ./traffic -count=1; then failed=1; fi
[ "$failed" -eq 0 ] || { echo "Tests failed. Logs: $log_dir" >&2; exit 1; }
echo "All isolated tests passed. Logs: $log_dir"

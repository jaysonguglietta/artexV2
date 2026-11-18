#!/bin/sh
# Run trusted integration tests against disposable databases, never a real DSN.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
container="artex-tests-$(date +%s)-$$"
log_dir=$(mktemp -d "${TMPDIR:-/tmp}/artex-tests.XXXXXX")
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT HUP INT TERM
# Test-only credential: the container is disposable and bound exclusively to loopback.
docker run -d --name "$container" --label artex.purpose=isolated-tests \
  --memory=1g --cpus=1 -e POSTGRES_USER=artex_test \
  -e POSTGRES_PASSWORD=local-test-only -e POSTGRES_DB=postgres \
  -p 127.0.0.1::5432 --tmpfs /var/lib/postgresql/data:rw,size=768m \
  postgres:16-alpine@sha256:81e77a4833487b8132fe4b00bedd39dc3f74b8641e387f19312a48f7de218c2e >/dev/null
attempt=0
until docker exec "$container" pg_isready -U artex_test >/dev/null 2>&1; do
  attempt=$((attempt+1)); [ "$attempt" -lt 30 ] || { echo "PostgreSQL failed to start" >&2; exit 1; }
  sleep 1
done
port=$(docker port "$container" 5432/tcp | sed 's/.*://')
failed=0
for package in db agent config evidence llmrec server; do
  docker exec "$container" createdb -U artex_test "artex_$package"
  echo "Testing $package (isolated database); logs: $log_dir/$package.jsonl"
  if ! ARTEX_ISOLATED_TEST_DATABASE=1 ARTEX_ALLOW_HOST_EXECUTION=1 ARTEX_PG_DSN="postgres://artex_test:local-test-only@127.0.0.1:$port/artex_$package?sslmode=disable" \
    go test "./$package" -count=1 -timeout=6m -json > "$log_dir/$package.jsonl"; then
    failed=1
    echo "FAILED: $package; see $log_dir/$package.jsonl" >&2
  fi
done
# Run remaining packages without a database configuration.
if ! ARTEX_PG_DSN= go test ./cmd/artex ./guard ./intercept ./llmpool ./mcphttp ./notify ./report ./selfupdate ./sidequestion ./traffic -count=1; then failed=1; fi
[ "$failed" -eq 0 ] || { echo "Tests failed. Logs: $log_dir" >&2; exit 1; }
echo "All isolated tests passed. Logs: $log_dir"

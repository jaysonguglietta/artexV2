#!/usr/bin/env bash
# Reset the fixed ARTEX account password using PostgreSQL pgcrypto bcrypt.
# Login hashes are stored in settings under auth.password_hash. Existing sessions
# are invalidated by password changes without restarting the API.
#
# Usage:
#   ./reset-password.sh                       # Detect config; prompt securely
#   ./reset-password.sh --dsn postgres://u:p@host:5432/artex
#   ./reset-password.sh -H 127.0.0.1 -P 5433 -U artex -W DBPASS -d artex
#   ./reset-password.sh -m docker             # Compose postgres service
#   ./reset-password.sh -m docker -c NAME --exec docker
#
# Options: --mode local|docker, --dsn, --host, --port, --user, --db-password,
# --dbname, --sslmode, --config, --container, --exec compose|docker, --yes.
# Prefer the interactive password prompt. Explicit password/DSN arguments are
# visible in process listings. psql receives new password data through the
# environment and escaped SQL variables; database roles need pgcrypto privileges.
set -euo pipefail

PASS_KEY="auth.password_hash"
BCRYPT_COST=10

MODE=""            # local | docker; empty selects automatically
DSN=""
HOST="" PORT="" USER="" DBPASS="" DBNAME="" SSLMODE=""
CONFIG=""
CONTAINER=""       # PostgreSQL service/container; defaults to postgres
EXEC_KIND=""       # compose | docker; empty selects automatically
NEWPASS=""
ASSUME_YES=0

die() { echo "Error: $*" >&2; exit 1; }
info() { echo "· $*" >&2; }

usage() { sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

# Parse command-line options.
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--mode)        MODE="${2:-}"; shift 2 ;;
    --dsn)            DSN="${2:-}"; shift 2 ;;
    -H|--host)        HOST="${2:-}"; shift 2 ;;
    -P|--port)        PORT="${2:-}"; shift 2 ;;
    -U|--user)        USER="${2:-}"; shift 2 ;;
    -W|--db-password) DBPASS="${2:-}"; shift 2 ;;
    -d|--dbname)      DBNAME="${2:-}"; shift 2 ;;
    --sslmode)        SSLMODE="${2:-}"; shift 2 ;;
    --config)         CONFIG="${2:-}"; shift 2 ;;
    -c|--container)   CONTAINER="${2:-}"; shift 2 ;;
    --exec)           EXEC_KIND="${2:-}"; shift 2 ;;
    -p|--new-password) NEWPASS="${2:-}"; shift 2 ;;
    -y|--yes)         ASSUME_YES=1; shift ;;
    -h|--help)        usage ;;
    *) die "Unknown argument: $1 (see -h)" ;;
  esac
done

# Read config.json only when local connection settings were not explicit.
# Prefer JSON parsing; the legacy grep fallback handles simple split fields only.
read_config_json() {
  local path="$1"
  [[ -f "$path" ]] || return 1
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$path" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1])).get("database", {})
except Exception:
    sys.exit(1)
# Accept a complete DSN or split connection fields.
if d.get("dsn"):
    print("DSN\t" + d["dsn"]); sys.exit(0)
for k in ("host","port","user","password","dbname","sslmode"):
    if d.get(k) is not None:
        print(k.upper() + "\t" + str(d[k]))
PY
  else
    # Fallback: read simple string/numeric fields individually.
    local k
    for k in host port user password dbname sslmode; do
      local v
      v=$(grep -oE "\"$k\"[[:space:]]*:[[:space:]]*(\"[^\"]*\"|[0-9]+)" "$path" 2>/dev/null \
            | head -1 | sed -E "s/.*:[[:space:]]*//; s/^\"//; s/\"$//") || true
      [[ -n "$v" ]] && echo -e "${k^^}\t$v"
    done
  fi
}

apply_config_fields() {
  local line key val
  while IFS=$'\t' read -r key val; do
    [[ -z "$key" ]] && continue
    case "$key" in
      DSN)      [[ -z "$DSN" ]] && DSN="$val" ;;
      HOST)     [[ -z "$HOST" ]] && HOST="$val" ;;
      PORT)     [[ -z "$PORT" ]] && PORT="$val" ;;
      USER)     [[ -z "$USER" ]] && USER="$val" ;;
      PASSWORD) [[ -z "$DBPASS" ]] && DBPASS="$val" ;;
      DBNAME)   [[ -z "$DBNAME" ]] && DBNAME="$val" ;;
      SSLMODE)  [[ -z "$SSLMODE" ]] && SSLMODE="$val" ;;
    esac
  done
}

# Detect deployment mode.
if [[ -z "$MODE" ]]; then
  if [[ -n "$DSN$HOST$USER$DBNAME" || -n "${ARTEX_PG_DSN:-}" || -f "${CONFIG:-config.json}" ]]; then
    MODE="local"
  elif command -v docker >/dev/null 2>&1 && [[ -f docker-compose.yml ]]; then
    MODE="docker"
  else
    MODE="local"
  fi
fi
info "Deployment mode: $MODE"

# Collect and confirm the new password.
if [[ -z "$NEWPASS" ]]; then
  read -r -s -p "New password (fixed username ARTEX):" NEWPASS; echo >&2
  [[ -n "$NEWPASS" ]] || die "Password must not be empty"
  read -r -s -p "Re-enter to confirm:" NEWPASS2; echo >&2
  [[ "$NEWPASS" == "$NEWPASS2" ]] || die "Passwords do not match"
fi
[[ -n "$NEWPASS" ]] || die "Password must not be empty"

# Pass password data to psql through its environment, not a new argv field.
export ARTEX_RESET_NEWPASS="$NEWPASS"

# Generate bcrypt in PostgreSQL and upsert using escaped psql variables.
# CREATE EXTENSION is idempotent but requires database-role privileges.
SQL=$(cat <<SQL
\\set ON_ERROR_STOP on
\\getenv newpw ARTEX_RESET_NEWPASS
CREATE EXTENSION IF NOT EXISTS pgcrypto;
INSERT INTO settings(key, value)
VALUES ('$PASS_KEY', crypt(:'newpw', gen_salt('bf', $BCRYPT_COST)))
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
SQL
)

# Execute against the chosen deployment.
if [[ "$MODE" == "local" ]]; then
  # Precedence: explicit fields, DSN/environment, config.json.
  if [[ -z "$DSN" && -z "$HOST$USER$DBNAME" ]]; then
    [[ -n "${ARTEX_PG_DSN:-}" ]] && DSN="$ARTEX_PG_DSN"
  fi
  if [[ -z "$DSN" && -z "$HOST$USER$DBNAME" ]]; then
    cfg="${CONFIG:-config.json}"
    if [[ -f "$cfg" ]]; then
      info "Reading database configuration from $cfg"
      apply_config_fields < <(read_config_json "$cfg")
    fi
  fi

  command -v psql >/dev/null 2>&1 || die "psql is unavailable (install postgresql-client or use -m docker)"

  declare -a PSQL_ARGS=()
  if [[ -n "$DSN" ]]; then
    PSQL_ARGS=("$DSN")
    target="$DSN"
  else
    [[ -n "$USER"   ]] || die "Database user (-U) or a valid config.json/DSN is required"
    [[ -n "$DBNAME" ]] || die "Database name (-d) or a valid config.json/DSN is required"
    HOST="${HOST:-127.0.0.1}"; PORT="${PORT:-5432}"; SSLMODE="${SSLMODE:-disable}"
    PSQL_ARGS=(-h "$HOST" -p "$PORT" -U "$USER" -d "$DBNAME")
    [[ -n "$SSLMODE" ]] && export PGSSLMODE="$SSLMODE"
    [[ -n "$DBPASS" ]] && export PGPASSWORD="$DBPASS"
    target="$USER@$HOST:$PORT/$DBNAME"
  fi

  info "Target database: $target"
  if [[ "$ASSUME_YES" -ne 1 ]]; then
    read -r -p "Reset the ARTEX password in this database? [y/N]" ans
    [[ "$ans" == "y" || "$ans" == "Y" ]] || die "Canceled"
  fi

  if ! printf '%s\n' "$SQL" | psql "${PSQL_ARGS[@]}" -v ON_ERROR_STOP=1 -q >/dev/null; then
    die "Write failed. If pgcrypto is missing or forbidden, use a role with extension privileges or install pgcrypto explicitly first."
  fi

else
  # ---- docker ----
  command -v docker >/dev/null 2>&1 || die "docker is unavailable"
  CONTAINER="${CONTAINER:-postgres}"

  # Prefer Compose service execution, otherwise use the explicit container.
  if [[ -z "$EXEC_KIND" ]]; then
    if docker compose version >/dev/null 2>&1 && [[ -f docker-compose.yml ]]; then
      EXEC_KIND="compose"
    else
      EXEC_KIND="docker"
    fi
  fi

  # Container credentials: explicit options, .env POSTGRES_*, then Compose defaults.
  if [[ -f .env ]]; then
    # shellcheck disable=SC1091
    set -a; . ./.env; set +a
  fi
  DUSER="${USER:-${POSTGRES_USER:-artex}}"
  DNAME="${DBNAME:-${POSTGRES_DB:-artex}}"
  [[ -n "$DBPASS" ]] && export PGPASSWORD="$DBPASS"
  [[ -z "${PGPASSWORD:-}" && -n "${POSTGRES_PASSWORD:-}" ]] && export PGPASSWORD="$POSTGRES_PASSWORD"

  info "Target: container $CONTAINER, psql -U $DUSER -d $DNAME (exec=$EXEC_KIND)"
  if [[ "$ASSUME_YES" -ne 1 ]]; then
    read -r -p "Reset the ARTEX password in this container database? [y/N]" ans
    [[ "$ans" == "y" || "$ans" == "Y" ]] || die "Canceled"
  fi

  # Pass environment names, not values, so passwords stay out of Docker argv.
  declare -a EXEC_CMD
  if [[ "$EXEC_KIND" == "compose" ]]; then
    EXEC_CMD=(docker compose exec -T -e ARTEX_RESET_NEWPASS -e PGPASSWORD "$CONTAINER"
              /usr/local/bin/artex-postgres-entrypoint --client /usr/local/bin/psql -U "$DUSER" -d "$DNAME" -v ON_ERROR_STOP=1 -q)
  else
    EXEC_CMD=(docker exec -i -e ARTEX_RESET_NEWPASS -e PGPASSWORD "$CONTAINER"
              /usr/local/bin/artex-postgres-entrypoint --client /usr/local/bin/psql -U "$DUSER" -d "$DNAME" -v ON_ERROR_STOP=1 -q)
  fi

  if ! printf '%s\n' "$SQL" | "${EXEC_CMD[@]}" >/dev/null; then
    die "Write failed. Use the hardened PostgreSQL image/client wrapper and check container name, credentials and pgcrypto privileges."
  fi
fi

unset ARTEX_RESET_NEWPASS
echo "ARTEX admin password reset. Log in as ARTEX with the new password; no service restart is required."
echo "  Existing sessions are invalidated within seconds. After account compromise, review custom tools, MCP, model and notification settings as well."

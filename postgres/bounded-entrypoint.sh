#!/bin/sh
# Trusted bootstrap only. PostgreSQL itself runs with UID postgres and zero caps.
set -eu
umask 077
if [ "$#" -eq 0 ]; then set -- postgres; fi
fail() { echo "[pgdisk] $*" >&2; exit 1; }
image_dir=/var/lib/artex-pgdisk
image=$image_dir/postgres.ext4
metadata=$image_dir/format-v1
mountpoint=/var/lib/postgresql/bounded
export PGDATA=$mountpoint/data

if [ "${1:-}" = --run-postgres ]; then
  shift
  [ "$(id -u)" = "$(id -u postgres)" ] || fail 'PostgreSQL must run as its dedicated user'
  for field in CapEff CapPrm CapInh CapBnd CapAmb; do
    value=$(awk -v name="$field:" '$1==name {print $2}' /proc/self/status)
    [ "$value" = 0000000000000000 ] || fail "PostgreSQL retained $field"
  done
  [ "$(awk '$1=="NoNewPrivs:" {print $2}' /proc/self/status)" = 1 ] || fail 'no-new-privileges is required'
  if [ -r /sys/module/apparmor/parameters/enabled ] && [ "$(cat /sys/module/apparmor/parameters/enabled)" = Y ]; then
    [ "$(cat /proc/self/attr/apparmor/current 2>/dev/null || cat /proc/self/attr/current)" = 'docker-default (enforce)' ] || fail 'PostgreSQL must inherit enforcing docker-default AppArmor'
  fi
  # Keep the exclusive backing lock in PID1; PostgreSQL may close inherited
  # descriptors. Both supervisor and server have the same unprivileged UID.
  /usr/local/bin/docker-entrypoint.sh "$@" &
  child=$!
  trap 'kill -TERM "$child" 2>/dev/null || true' TERM
  trap 'kill -INT "$child" 2>/dev/null || true' INT
  trap 'kill -HUP "$child" 2>/dev/null || true' HUP
  while :; do
    if wait "$child"; then status=0; else status=$?; fi
    if ! kill -0 "$child" 2>/dev/null; then exit "$status"; fi
  done
fi

[ "$(id -u)" = 0 ] || fail 'bounded filesystem bootstrap requires root'
# The upstream image declares a legacy VOLUME. Never expose that unbounded
# writable directory to PostgreSQL or silently adopt an old mounted database.
legacy=/var/lib/postgresql/data
[ -d "$legacy" ] && [ ! -L "$legacy" ] || fail 'legacy data mount must be an ordinary directory'
[ -z "$(find "$legacy" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail 'legacy PGDATA is nonempty; use explicit offline migration, never automatic adoption'
chown 0:0 "$legacy"
chmod 000 "$legacy"
for tool in dd mkfs.ext4 blkid losetup mount umount findmnt flock setpriv; do
  command -v "$tool" >/dev/null || fail "required bootstrap tool missing: $tool"
done
[ -d "$image_dir" ] && [ ! -L "$image_dir" ] || fail 'backing volume must be an ordinary directory'
[ -d "$mountpoint" ] && [ ! -L "$mountpoint" ] || fail 'bounded mountpoint must already exist in image'
chmod 0700 "$image_dir"
[ ! -L "$image_dir/lock" ] || fail 'backing lock is a symlink'
if [ -e "$image_dir/lock" ] && [ ! -f "$image_dir/lock" ]; then fail 'backing lock is not an ordinary file'; fi
[ ! -e "$image_dir/migration-pending" ] && [ ! -L "$image_dir/migration-pending" ] || fail 'incomplete migration requires operator inspection; database will not start'
exec 9>"$image_dir/lock"
flock -n 9 || fail 'backing filesystem is already in use'
bytes=${ARTEX_PG_DISK_BYTES:-8589934592}
case "$bytes" in ''|*[!0-9]*) fail 'ARTEX_PG_DISK_BYTES must be a decimal integer';; esac
[ "$bytes" -ge 536870912 ] && [ "$bytes" -le 1099511627776 ] || fail 'disk bound must be between 512 MiB and 1 TiB'
[ $((bytes % 1048576)) = 0 ] || fail 'disk bound must be a whole MiB'
[ ! -L "$image" ] && [ ! -L "$metadata" ] || fail 'backing image/metadata must not be symlinks'
new_image=0
if [ ! -e "$image" ] && [ ! -e "$metadata" ]; then
  new_image=1
  extra=$(find "$image_dir" -mindepth 1 -maxdepth 1 ! -name lock -print -quit)
  [ -z "$extra" ] || fail 'unknown backing volume contents; refusing initialization'
  (set -C; : > "$image_dir/creating") || fail 'cannot exclusively create backing image'
  dd if=/dev/zero of="$image_dir/creating" bs=1048576 count=$((bytes / 1048576)) conv=fsync status=none || fail 'preallocation failed; preserve creating file for operator inspection'
  mkfs.ext4 -q -F -m 1 -L artex-pgdisk -E nodiscard,lazy_itable_init=0,lazy_journal_init=0 "$image_dir/creating"
  uuid=$(blkid -s UUID -o value "$image_dir/creating")
  printf 'ARTEX_PGDISK_V1\n%s\n%s\n' "$uuid" "$bytes" > "$image_dir/format-new"
  sync
  mv "$image_dir/creating" "$image"
  mv "$image_dir/format-new" "$metadata"
  sync
fi
[ -f "$image" ] && [ -f "$metadata" ] || fail 'incomplete/unknown backing state; never automatically recreate existing data'
[ "$(stat -c '%u:%g:%a' "$image")" = 0:0:600 ] || fail 'backing image must be root:root mode0600'
[ "$(wc -l < "$metadata" | tr -d ' ')" = 3 ] || fail 'invalid backing metadata'
[ "$(sed -n '1p' "$metadata")" = ARTEX_PGDISK_V1 ] || fail 'unknown backing metadata version'
uuid=$(sed -n '2p' "$metadata")
[ "$(sed -n '3p' "$metadata")" = "$bytes" ] || fail 'configured disk size differs from existing volume; explicit offline migration/expansion required'
[ "$(stat -c %s "$image")" = "$bytes" ] || fail 'backing image length changed'
allocated=$(( $(stat -c %b "$image") * 512 ))
[ "$allocated" -ge "$bytes" ] || fail 'backing image is sparse; physical preallocation required'
[ "$(blkid -s TYPE -o value "$image")" = ext4 ] || fail 'backing image is not ext4'
[ "$(blkid -s UUID -o value "$image")" = "$uuid" ] || fail 'backing filesystem identity changed'
loop=$(losetup -f | sed 's/ (lost)$//')
number=${loop#/dev/loop}
case "$number" in ''|*[!0-9]*) fail 'unexpected loop device';; esac
if [ ! -e "$loop" ]; then mknod -m 0600 "$loop" b 7 "$number"; fi
[ -b "$loop" ] && [ ! -L "$loop" ] || fail 'loop device must be a block device'
chmod 0600 "$loop"
losetup "$loop" "$image"
mounted=0
cleanup() {
  if [ "$mounted" = 1 ]; then umount "$mountpoint" || true; fi
  losetup -d "$loop" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
mount -t ext4 -o rw,nosuid,nodev,noexec "$loop" "$mountpoint"
mounted=1
[ "$(findmnt -n -o FSTYPE --target "$mountpoint")" = ext4 ] || fail 'filesystem proof failed'
[ "$(findmnt -n -o SOURCE --target "$mountpoint")" = "$loop" ] || fail 'mount source proof failed'
# Mark loop autoclear while the mounted filesystem retains its reference. Kernel
# namespace teardown releases it after PostgreSQL exits, including container kill.
losetup -d "$loop"
[ "$(cat "/sys/block/loop$number/loop/autoclear")" = 1 ] || fail 'loop autoclear was not established'
reserve=$mountpoint/.artex-recovery-reserve
reserve_state=$image_dir/recovery-reserve-v1
zero_hash=3b6a07d0d404fab4e23b6d34bc6696a6a312dd92821332385e5af7c01c421351
if [ "$new_image" = 1 ]; then
  [ ! -e "$reserve" ] && [ ! -e "$reserve_state" ] || fail 'unexpected recovery reserve on a new filesystem'
  dd if=/dev/zero of="$reserve" bs=1048576 count=64 conv=fsync status=none
  chmod 0600 "$reserve"
  printf 'allocated\n' > "$reserve_state"
  sync
fi
[ -f "$reserve_state" ] && [ ! -L "$reserve_state" ] || fail 'recovery reserve state missing or invalid; inspect incomplete initialization'
state=$(cat "$reserve_state")
case "$state" in
  allocated)
    [ -f "$reserve" ] && [ ! -L "$reserve" ] || fail 'allocated recovery reserve missing'
    [ "$(stat -c '%u:%g:%a:%s:%h' "$reserve")" = 0:0:600:67108864:1 ] || fail 'recovery reserve properties changed'
    [ "$(sha256sum "$reserve" | awk '{print $1}')" = "$zero_hash" ] || fail 'recovery reserve no longer contains only generated zeros'
    ;;
  released) [ ! -e "$reserve" ] && [ ! -L "$reserve" ] || fail 'unexpected file after recovery reserve release';;
  *) fail 'unknown recovery reserve state';;
esac
if [ "${1:-}" = --release-recovery-reserve ]; then
  [ "$state" = allocated ] || fail 'recovery reserve was already released; it is never recreated automatically'
  rm "$reserve"
  printf 'released\n' > "$image_dir/recovery-reserve-new"
  mv "$image_dir/recovery-reserve-new" "$reserve_state"
  sync
  printf 'recovery_reserve_released_bytes=67108864\nuser_data_deleted=false\n'
  exit 0
fi
if [ "${1:-}" = --inspect ]; then
  printf 'filesystem=ext4\nbytes=%s\nuuid=%s\nrecovery_reserve=%s\n' "$bytes" "$uuid" "$state"
  df -Pk "$mountpoint"
  exit 0
fi
[ ! -L "$PGDATA" ] || fail 'PGDATA must not be a symlink'
if [ "${1:-}" = --migrate ]; then
  [ -d /legacy-pgdata ] && [ ! -L /legacy-pgdata ] || fail 'migration source must be a read-only ordinary directory'
  [ ! -e "$PGDATA" ] || fail 'migration target already has data; refusing replacement'
  [ "$(cat /legacy-pgdata/PG_VERSION)" = 16 ] || fail 'only PostgreSQL16 physical migration is supported'
  [ ! -e /legacy-pgdata/postmaster.pid ] || fail 'source was not cleanly stopped; use a verified logical restore/recovery instead'
  [ -z "$(find /legacy-pgdata -type l -print -quit)" ] || fail 'source has external/symlink storage; explicit logical migration required'
  [ -z "$(find /legacy-pgdata ! -type d ! -type f -print -quit)" ] || fail 'source has special files'
  source_kib=$(du -sk /legacy-pgdata | awk '{print $1}')
  available_kib=$(df -Pk "$mountpoint" | awk 'NR==2 {print $4}')
  [ "$source_kib" -le $((available_kib * 8 / 10)) ] || fail 'source does not fit with20percent recovery headroom'
  printf 'ARTEX_MIGRATION_PENDING_V1\n' > "$image_dir/migration-pending"
  sync
  mkdir -m 0700 "$PGDATA"
  # Failure intentionally leaves this NEW target for inspection; source is never changed.
  cp -a /legacy-pgdata/. "$PGDATA/"
  export PGDATA
  find /legacy-pgdata -type f -exec sh -ec '
    for source do
      relative=${source#/legacy-pgdata/}
      cmp -s "$source" "$PGDATA/$relative" || exit 1
    done
  ' sh {} + || fail 'migration byte verification failed'
  chown -R postgres:postgres "$PGDATA"
  sync
  rm "$image_dir/migration-pending"
  sync
  printf 'migration=verified-byte-copy\nsource_retained=true\n'
  exit 0
fi
mkdir -p "$PGDATA"
chown postgres:postgres "$PGDATA" /var/run/postgresql
chmod 0700 "$PGDATA"
# FD9 deliberately survives exec: a second container cannot mount the same image.
setpriv_path=$(command -v setpriv)
if [ -r /sys/module/apparmor/parameters/enabled ] && [ "$(cat /sys/module/apparmor/parameters/enabled)" = Y ]; then
  attribute=/proc/self/attr/apparmor/exec
  [ -e "$attribute" ] || attribute=/proc/self/attr/exec
  printf 'exec docker-default' > "$attribute" || fail 'cannot re-enter docker-default; run scripts/setup-postgres.sh first'
fi
exec "$setpriv_path" --reuid=postgres --regid=postgres --clear-groups \
  --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs \
  /usr/local/bin/artex-postgres-entrypoint --run-postgres "$@"

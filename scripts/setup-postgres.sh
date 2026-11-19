#!/bin/sh
# Read-only preflight; never creates/adopts/deletes a database volume.
set -eu
image=${ARTEX_POSTGRES_IMAGE:-artex-local:postgres}
docker image inspect "$image" >/dev/null
# A normal confined helper asks Docker to load docker-default on a fresh daemon.
# The fixed mount bootstrap later re-enters that same profile before running PG.
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --user 70:70 \
  --log-driver local --log-opt max-size=2m --log-opt max-file=2 \
  --entrypoint /bin/true "$image"
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --device /dev/loop-control \
  --log-driver local --log-opt max-size=2m --log-opt max-file=2 \
  --entrypoint /bin/sh "$image" -ec 'test -c /dev/loop-control; command -v mkfs.ext4 >/dev/null; command -v setpriv >/dev/null'
echo 'PostgreSQL bounded-filesystem tools, loop-control and normal confinement preflight passed.'

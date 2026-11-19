#!/bin/sh
# Native Ubuntu builder only; no build tools or source tree enter the worker.
set -eu
export LC_ALL=C.UTF-8
export DEB_BUILD_PROFILES=noudeb
export DEB_BUILD_OPTIONS=parallel=2
artifacts=${1:?verified source archive directory is required}
output=${2:?package output directory is required}
case "$(dpkg --print-architecture)" in amd64|arm64) ;; *) exit 1 ;; esac
work=$(mktemp -d /tmp/artex-util-linux.XXXXXX)
source=$(python3 /opt/artex-util-linux/prepare-source.py "$artifacts" "$work")
cd "$source"
# Capture verbose compiler/test output instead of losing it to BuildKit log caps.
if ! dpkg-buildpackage --build=any,source --no-sign > "$work/build.log" 2>&1; then
    tail -n 100 "$work/build.log" >&2
    exit 1
fi
mkdir -p "$output/debs" "$output/source"
for package in bsdutils libblkid1 libmount1 libsmartcols1 libuuid1 login mount util-linux; do
    set -- "$work/${package}_"*.deb
    test "$#" -eq 1 && test -f "$1"
    cp "$1" "$output/debs/"
done
cp "$work/"*.dsc "$work/"*.debian.tar.xz "$work/"*.orig.tar.xz "$work/"*.buildinfo "$work/"*.changes "$output/source/"
# Preserve the actual package manifests; never relabel an old binary as fixed.
for package in "$output/debs/"*.deb; do
    case "$(dpkg-deb -f "$package" Package)" in
        bsdutils) expected=1:2.42.4-0artex1 ;;
        login) expected=1:4.16.0-2+really2.42.4-0artex1 ;;
        *) expected=2.42.4-0artex1 ;;
    esac
    test "$(dpkg-deb -f "$package" Version)" = "$expected"
    identity=$(dpkg-deb -f "$package" Source | sed 's/ (.*//')
    test -n "$identity" || identity=$(dpkg-deb -f "$package" Package)
    test "$identity" = util-linux
    dpkg-deb -f "$package" Package Version Source Architecture
done

cp "$work/build.log" "$output/source/"
printf '%s\n' "$source" > "$output/build-source.path"
cp /opt/artex-util-linux/ubuntu-source-authentication.json /opt/artex-util-linux/source.json "$output/source/"

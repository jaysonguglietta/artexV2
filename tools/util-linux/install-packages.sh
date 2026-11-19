#!/bin/sh
# Run only while building the final worker, after its last Ubuntu APT install.
set -eu
export DEBIAN_FRONTEND=noninteractive
packages=${1:?directory of the eight local Debian packages is required}
apt-get update
# Ubuntu's minimal-image dpkg exclusions would otherwise omit files recorded in
# the new packages' md5sums. Retain their licenses, provenance and manuals.
apt-get install -y --no-install-recommends \
    -o 'DPkg::Options::=--path-include=/usr/share/doc/*' \
    -o 'DPkg::Options::=--path-include=/usr/share/man/*' \
    "$packages/"*.deb
for package in bsdutils libblkid1 libmount1 libsmartcols1 libuuid1 login mount util-linux; do
    case "$package" in
        bsdutils) expected=1:2.42.4-0artex1 ;;
        login) expected=1:4.16.0-2+really2.42.4-0artex1 ;;
        *) expected=2.42.4-0artex1 ;;
    esac
    test "$(dpkg-query -W -f='${Version}' "$package")" = "$expected"
    test "$(dpkg-query -W -f='${source:Package}' "$package")" = util-linux
    test "$(dpkg-query -W -f='${source:Version}' "$package")" = 2.42.4-0artex1
    verified=$(dpkg --verify "$package")
    test -z "$verified"
done
rm -rf /var/lib/apt/lists/*

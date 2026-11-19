#!/bin/sh
# Build real architecture-specific packages; never overlay a stale system libjq.
set -eu
umask 022
[ "$#" = 2 ] || { echo 'Usage: build-debs.sh VERIFIED_SOURCE_TAR EMPTY_OUTPUT_DIRECTORY' >&2; exit 1; }
source_archive=$(realpath "$1")
output=$(realpath -m "$2")
[ ! -e "$output" ] || [ -z "$(find "$output" -mindepth 1 -maxdepth 1 -print -quit)" ] || {
  echo 'Package output must be new or empty' >&2; exit 1;
}
mkdir -p "$output"
printf '%s  %s\n' 71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed095abd315e26e6e1193af3af "$source_archive" | sha256sum -c -
architecture=$(dpkg --print-architecture)
case "$architecture" in amd64|arm64) ;; *) echo 'Unsupported package architecture' >&2; exit 1;; esac
triplet=$(dpkg-architecture -qDEB_HOST_MULTIARCH)
version=1.8.2-0artex1
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
mkdir "$build_dir/source" "$build_dir/installed"
tar -xzf "$source_archive" --strip-components=1 --no-same-owner --no-same-permissions -C "$build_dir/source"
cd "$build_dir/source"
grep '^#define JQ_VERSION "1.8.2"$' src/version.h
export DEB_BUILD_MAINT_OPTIONS=hardening=+all
CFLAGS=$(dpkg-buildflags --get CFLAGS)
CPPFLAGS=$(dpkg-buildflags --get CPPFLAGS)
LDFLAGS=$(dpkg-buildflags --get LDFLAGS)
export CFLAGS CPPFLAGS LDFLAGS
./configure --prefix=/usr --libdir="/usr/lib/$triplet" --enable-shared --disable-static \
  --disable-docs --disable-maintainer-mode --with-oniguruma=yes
# Refuse fallback to the bundled regex library; the system library is inventoried
# and maintained independently by Ubuntu rather than hidden inside our package.
grep -E '^onig_LDFLAGS = *$' Makefile
make -j2
make -j2 check
make DESTDIR="$build_dir/installed" install
library="$build_dir/installed/usr/lib/$triplet/libjq.so.1.0.4"
[ -f "$library" ] && [ -f "$build_dir/installed/usr/bin/jq" ]
readelf -d "$library" | grep 'Shared library: \[libonig.so.5\]'
readelf -d "$library" | grep 'Library soname: \[libjq.so.1\]'
# Fail on any change to the reviewed Ubuntu1.8.1/upstream1.8.2 exported-symbol
# baseline on either native architecture. This checks names, not C ABI semantics.
printf '%s  %s\n' 281c4c47bd8445040bd4a7cc94f44b9b75c0f6a254d89ed12d6a5711c459d0db /build/jq/abi-symbols.txt | sha256sum -c -
nm -D --defined-only "$library" > "$build_dir/exports.raw"
awk '$3 ~ /^(jq_|jv_)/ { print $3 }' "$build_dir/exports.raw" | LC_ALL=C sort -u > "$build_dir/exports.txt"
cmp /build/jq/abi-symbols.txt "$build_dir/exports.txt"
strip --strip-unneeded "$build_dir/installed/usr/bin/jq" "$library"

mkdir -p debian
cat > debian/control <<EOF
Source: jq
Section: utils
Priority: optional
Maintainer: ARTEX Security Maintainers <security@artex.invalid>
Standards-Version: 4.7.2

Package: jq
Architecture: any
Description: JSON processor built from verified upstream jq1.8.2

Package: libjq1
Architecture: any
Description: Shared jq runtime built from verified upstream jq1.8.2
EOF
printf 'libjq 1 libjq1 (= %s)\n' "$version" > debian/shlibs.local
dpkg-shlibdeps -O "-e$build_dir/installed/usr/bin/jq" "-l$build_dir/installed/usr/lib/$triplet" > "$build_dir/cli-dependencies"
dpkg-shlibdeps -O "-e$library" > "$build_dir/library-dependencies"
cli_dependencies=$(sed -n 's/^shlibs:Depends=//p' "$build_dir/cli-dependencies")
library_dependencies=$(sed -n 's/^shlibs:Depends=//p' "$build_dir/library-dependencies")
[ -n "$cli_dependencies" ] && [ -n "$library_dependencies" ]
case "$cli_dependencies" in *"libjq1 (= $version)"*) ;; *) echo 'jq dependency does not bind the new shared library' >&2; exit 1;; esac
case "$library_dependencies" in *libonig5*) ;; *) echo 'Missing system regex dependency' >&2; exit 1;; esac

for package in jq libjq1; do
  package_root="$build_dir/$package"
  mkdir -p "$package_root/DEBIAN" "$package_root/usr/share/doc/$package"
  cp COPYING "$package_root/usr/share/doc/$package/copyright"
  cp NEWS.md AUTHORS "$package_root/usr/share/doc/$package/"
  cp /build/jq/source.json "$package_root/usr/share/doc/$package/artex-source.json"
  if [ "$package" = jq ]; then
    mkdir -p "$package_root/usr/bin" "$package_root/usr/share/man/man1"
    cp "$build_dir/installed/usr/bin/jq" "$package_root/usr/bin/jq"
    gzip -9n < jq.1 > "$package_root/usr/share/man/man1/jq.1.gz"
    dependencies=$cli_dependencies
    multi_arch=foreign
    description='Command-line JSON processor built from verified upstream jq1.8.2'
  else
    mkdir -p "$package_root/usr/lib/$triplet"
    cp "$library" "$package_root/usr/lib/$triplet/libjq.so.1.0.4"
    ln -s libjq.so.1.0.4 "$package_root/usr/lib/$triplet/libjq.so.1"
    printf 'libjq 1 libjq1 (>= %s)\n' "$version" > "$package_root/DEBIAN/shlibs"
    printf 'activate-noawait ldconfig\n' > "$package_root/DEBIAN/triggers"
    dependencies=$library_dependencies
    multi_arch=same
    description='Shared JSON query runtime built from verified upstream jq1.8.2'
  fi
  installed_size=$(du -sk "$package_root/usr" | cut -f1)
  cat > "$package_root/DEBIAN/control" <<EOF
Package: $package
Source: jq
Version: $version
Architecture: $architecture
Multi-Arch: $multi_arch
Section: utils
Priority: optional
Maintainer: ARTEX Security Maintainers <security@artex.invalid>
Installed-Size: $installed_size
Depends: $dependencies
Homepage: https://jqlang.org/
Description: $description
 Unmodified upstream code with ARTEX native package integration.
EOF
  (cd "$package_root" && find usr -type f -print0 | sort -z | xargs -0 md5sum) > "$package_root/DEBIAN/md5sums"
  dpkg-deb --root-owner-group --build "$package_root" "$output/${package}_${version}_${architecture}.deb"
done
(cd "$output" && sha256sum ./*.deb > SHA256SUMS)

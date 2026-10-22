#!/bin/bash
set -euo pipefail

fixture_dir=$(cd -- "$(dirname -- "$0")" && pwd)
source_dir=$(mktemp -d)
trap 'rm -rf "$source_dir"' EXIT
mkfs_erofs=${MKFS_EROFS:-mkfs.erofs}

build_fixture() {
  "$mkfs_erofs" -x-1 --all-root -T1700000000 \
    -U00000000-0000-0000-0000-000000000000 "$@"
}

mkdir -p "$source_dir/plain/usr/bin"
printf 'mock-nmstatectl-binary' > "$source_dir/plain/usr/bin/nmstatectl"
chmod 0755 "$source_dir/plain/usr/bin/nmstatectl"
build_fixture -b4096 "$fixture_dir/nmstatectl.erofs" "$source_dir/plain"

mkdir -p "$source_dir/compressed/usr/bin" "$source_dir/compressed/a"
awk 'BEGIN { for (i = 0; i < 8192; i++) print "mock-nmstatectl-binary" }' \
  > "$source_dir/compressed/usr/bin/nmstatectl"
chmod 0751 "$source_dir/compressed/usr/bin/nmstatectl"
ln -s ../usr/bin/nmstatectl "$source_dir/compressed/a/nmstatectl"
build_fixture -b4096 -zlzma "$fixture_dir/nmstatectl-lzma.erofs" "$source_dir/compressed"
build_fixture -b65536 -zlzma "$fixture_dir/nmstatectl-lzma-64k.erofs" "$source_dir/compressed"

mkdir -p "$source_dir/missing/usr/bin"
printf 'other binary' > "$source_dir/missing/usr/bin/other"
build_fixture -b4096 "$fixture_dir/nmstatectl-missing.erofs" "$source_dir/missing"

mkdir -p "$source_dir/nonregular/a" "$source_dir/nonregular/b/nmstatectl"
ln -s ../b/nmstatectl "$source_dir/nonregular/a/nmstatectl"
build_fixture -b4096 "$fixture_dir/nmstatectl-nonregular.erofs" "$source_dir/nonregular"

sha256sum "$fixture_dir/"*.erofs

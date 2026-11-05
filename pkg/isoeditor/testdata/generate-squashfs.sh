#!/bin/bash
set -euo pipefail

fixture_dir=$(cd -- "$(dirname -- "$0")" && pwd)
source_dir=$(mktemp -d)
trap 'rm -rf "$source_dir"' EXIT
mksquashfs=${MKSQUASHFS:-mksquashfs}

build_fixture() {
  find "$source_dir" -exec touch -h -d @1700000000 {} +
  "$mksquashfs" "$1" "$2" -noappend -processors 1 -comp zstd -Xcompression-level 3 \
    -b 4096 -no-xattrs -all-root -no-exports -no-fragments -no-recovery \
    -mkfs-time 1700000000 >/dev/null
}

mkdir -p "$source_dir/compressed/usr/bin" "$source_dir/compressed/a"
awk 'BEGIN { for (i = 0; i < 8192; i++) print "mock-nmstatectl-binary" }' \
  > "$source_dir/compressed/usr/bin/nmstatectl"
chmod 0751 "$source_dir/compressed/usr/bin/nmstatectl"
ln -s ../usr/bin/nmstatectl "$source_dir/compressed/a/nmstatectl"
build_fixture "$source_dir/compressed" "$fixture_dir/nmstatectl-zstd.squashfs"

mkdir -p "$source_dir/missing/usr/bin"
printf 'other binary' > "$source_dir/missing/usr/bin/other"
build_fixture "$source_dir/missing" "$fixture_dir/nmstatectl-missing.squashfs"

mkdir -p "$source_dir/nonregular/a" "$source_dir/nonregular/b/nmstatectl"
ln -s ../b/nmstatectl "$source_dir/nonregular/a/nmstatectl"
build_fixture "$source_dir/nonregular" "$fixture_dir/nmstatectl-nonregular.squashfs"

cp "$fixture_dir/nmstatectl-zstd.squashfs" "$fixture_dir/nmstatectl-malformed.squashfs"
truncate -s 512 "$fixture_dir/nmstatectl-malformed.squashfs"

sha256sum "$fixture_dir/"*.squashfs

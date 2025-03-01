#!/bin/bash -ex

# Details of the package:
version="4.6.1"
tarball="${version}.tar.gz"
location="https://github.com/plougher/squashfs-tools/archive/refs/tags/${version}.tar.gz"
checksum="94201754b36121a9f022a190c75f718441df15402df32c2b520ca331a107511c"

# Download the sources:
if [ ! -f "${tarball}" ]; then
  curl --silent --output "${tarball}" --location "${location}"
fi

# Verify the checksum:
echo "${checksum} ${tarball}" | sha256sum --check

# Build the artifacts:
rm -rf build
mkdir build
tar --directory build --extract --strip-components 1 --file "${tarball}"
pushd build/squashfs-tools
export XZ_SUPPORT=1
export LZO_SUPPORT=1
export LZMA_XZ_SUPPORT=1
export LZ4_SUPPORT=1
export ZSTD_SUPPORT=1
make
cp unsquashfs /usr/local/bin
popd

#!/bin/bash -ex

# Details of the package:
version="5.6.2"
tarball="xz-${version}.tar.gz"
location="https://github.com/tukaani-project/xz/releases/download/v${version}/${tarball}"
checksum="8bfd20c0e1d86f0402f2497cfa71c6ab62d4cd35fd704276e3140bfb71414519"

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
pushd build
./configure
make install
popd

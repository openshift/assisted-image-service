#!/bin/bash -ex

# Details of the package:
version="1.9.4"
tarball="erofs-utils-${version}.tar.gz"
checksum="7d135aa2550326a5acf20f53c518aea5a8900015ce50700044e40f818c31dd80"
location="https://git.kernel.org/pub/scm/linux/kernel/git/xiang/erofs-utils.git/snapshot/erofs-utils-${version}.tar.gz"

# Download the sources if they aren't available yet:
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
./autogen.sh
./configure --with-selinux MAX_BLOCK_SIZE=65536
make install
popd

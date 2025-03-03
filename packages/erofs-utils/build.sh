#!/bin/bash -ex

# Details of the package:
version="1.8.5"
tarball="erofs-utils-${version}.tar.gz"
checksum="cd8611270e9c86fe062f647103ca6ada9ed710e4430fdd5960d514777919200d"
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
./configure --with-selinux
make install
popd

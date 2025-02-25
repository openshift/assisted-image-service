#!/bin/bash -ex

# We will be installing tools to the '/usr/local' directories, so we need to configure the package
# search path and generate the binaries so that they will look for dynamic libraries there.
export PKG_CONFIG_PATH="/usr/local/lib/pkgconfig"
export LDFLAGS="-Wl,-rpath=/usr/local/lib"

# Build the package, in the right order to account for build dependencies:
(cd xz && ./build.sh)
(cd erofs-utils && ./build.sh)
(cd squashfs-tools && ./build.sh)

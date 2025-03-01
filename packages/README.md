# External packages

This directory contains the source code of packages that we need, but aren't
available in the distribution that we use, so we build them from source.

Currently we need the following packages:

- `erofs-utils` - We need this to extract binaries from the root filesystem
  of the versions of CoreOS that we install, in particular the `nmstatectl`
  binary. The `erofs-utils` package is not yet included in RHEL 9.

- `xz` - We need this because the `erofs-utils` package requires at least
  version 5.3, which isn't currently included in RHEL 9.

## Building

To build and install all the packages run the `build.sh` script in this
directory. You can also go into the directory of a specific package and run the
`build.sh` script to build only that package.

Note that this will build the package and install them to `/usr/local`, which
usually requires root privileges.

## Upgrading

To update the version of one of the packages you need to download the new
version of the source and then edit the `build.sh` accordingly.

As the `build.sh` script tries to download the source if the doesn't exist the
easy way to upgrade (or downgrade) a version is first to remove the old source
and change only the `version` variable and run the script. For example, imagine
that you want to want to downgrade the `xz` package from 5.6.2 to 5.6.1:

```
$ cd xz
$ rm xz-*.tar.gz
$ sed -i 's/version=.*/version="5.6.1"/' build.sh
$ ./build.sh
```

That will fail, with an output similar to this, because the checksum will not
match the new sources:

```
+ version=5.6.1
+ file=xz-5.6.1.tar.gz
+ location=https://github.com/tukaani-project/xz/releases/download/v5.6.1/xz-5.6.1.tar.gz
+ checksum=8bfd20c0e1d86f0402f2497cfa71c6ab62d4cd35fd704276e3140bfb71414519
+ '[' '!' -f xz-5.6.1.tar.gz ']'
+ echo '8bfd20c0e1d86f0402f2497cfa71c6ab62d4cd35fd704276e3140bfb71414519 xz-5.6.1.tar.gz'
+ sha256sum --check
xz-5.6.1.tar.gz: FAILED
sha256sum: WARNING: 1 computed checksum did NOT match
```

The good part is that you will now have the new sources downloaded, so you can
calculate the checksum yourself:

```
$ sha256sum xz-5.6.1.tar.gz
0019dfc4b32d63c1392aa264aed2253c1e0c2fb09216f8e2cc269bbfb8bb49b5  xz-5.6.1.tar.gz
```

It is also good if you can use some other mechanism to verify that those
sources are correct. For example, the `xz` project publishes signatures in
their GitHub releases page. When submitting your patch ask your reviewers to
double check the correctness of the sources.

Once you have the checksum update the `checksum` variable in the script:

```
$ sed -i 's/checksum=.*/checksum=0019dfc4b32d63c1392aa264aed2253c1e0c2fb09216f8e2cc269bbfb8bb49b5/' build.sh
```

Finally, remember to add the new sources to the repository:

```
$ git add xz-5.6.1.tar.gz
```

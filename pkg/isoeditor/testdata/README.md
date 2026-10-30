# EROFS nmstatectl fixtures

These fixtures contain a regular `usr/bin/nmstatectl` file with mode `0755` and
the content `mock-nmstatectl-binary`. They are used to exercise the native
go-diskfs EROFS reader without requiring `erofs-utils` at test time.

Generate the source tree:

```sh
mkdir -p source/usr/bin
printf 'mock-nmstatectl-binary' > source/usr/bin/nmstatectl
chmod 0755 source/usr/bin/nmstatectl
```

Generate the fixtures with `mkfs.erofs` 1.9.4:

```sh
mkfs.erofs -b4096 -x-1 --all-root -T1700000000 \
  -U00000000-0000-0000-0000-000000000000 \
  nmstatectl.erofs source
mkfs.erofs -b4096 -zlzma -x-1 --all-root -T1700000000 \
  -U00000000-0000-0000-0000-000000000000 \
  nmstatectl-lzma.erofs source
```

SHA-256:

- `nmstatectl.erofs`: `a59127eb0a8b9cb3014394b41a49b8d1fe2e2ca79320c8e1651d7ff275ae5c34`
- `nmstatectl-lzma.erofs`: `c33e9ffb8af9a999ea6404a59f6a5dc2aec1a8636118f4caed58938912cd9769`

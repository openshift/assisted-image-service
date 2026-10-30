# EROFS nmstatectl fixtures

These fixtures exercise native extraction from a CPIO-wrapped EROFS filesystem.
Unit tests do not require erofs-utils or invoke any shell commands for EROFS.

| Fixture | Contents |
| --- | --- |
| `nmstatectl.erofs` | Uncompressed `usr/bin/nmstatectl`, 22 bytes, mode `0755` |
| `nmstatectl-lzma.erofs` | LZMA compact layout, 4 KiB blocks, 188416-byte binary, mode `0751` |
| `nmstatectl-lzma-64k.erofs` | LZMA full layout, 64 KiB blocks, same binary and mode |
| `nmstatectl-missing.erofs` | A regular file named `other`, with no `nmstatectl` |
| `nmstatectl-nonregular.erofs` | A directory and a symlink named `nmstatectl`, with no regular binary |

Both compressed fixtures contain `mock-nmstatectl-binary\n` repeated 8192 times,
plus an earlier symlink named `nmstatectl` that extraction must skip. The larger
payload ensures the binary is actually compressed. Different permissions verify
that the output archive retains the source mode.

Regenerate with erofs-utils 1.9.4, built with `MAX_BLOCK_SIZE=65536` and LZMA
support. The generator fixes ownership, timestamps and UUID:

```sh
MKFS_EROFS=/path/to/mkfs.erofs bash pkg/isoeditor/testdata/generate-erofs.sh
```

SHA-256:

- `nmstatectl.erofs`: `a59127eb0a8b9cb3014394b41a49b8d1fe2e2ca79320c8e1651d7ff275ae5c34`
- `nmstatectl-lzma.erofs`: `e75f99de1962f5f1606b84eacaa4a26ad3521123f9f4a68aae4e653df4de5b42`
- `nmstatectl-lzma-64k.erofs`: `6b1a36bc0d56b98b913bf3306c95b685a6bd27c92bea38e2c3d4c994bfd98fe6`
- `nmstatectl-missing.erofs`: `35c49d334b11a58b8d2b7e23fb71ebee579905f47a322a3fd01074e671c16b5e`
- `nmstatectl-nonregular.erofs`: `a4b47c3f357899b7e3c47a4d5e13b0cabc159227c5e0ef4267cd300857986a76`

## Optional real RHCOS ISO comparison

Supply a local EROFS-based RHCOS ISO and a reference binary independently
extracted from its `root.erofs`, preserving its permissions. For example:

```sh
fsck.erofs --extract=/path/to/nmstatectl.reference --preserve-perms \
  --path=/ostree/deploy/rhcos/deploy/DEPLOYMENT/usr/bin/nmstatectl /path/to/root.erofs
RHCOS_EROFS_ISO=/path/to/rhcos.iso \
  RHCOS_NMSTATECTL_REFERENCE=/path/to/nmstatectl.reference \
  go test -count=1 -run '^TestErofsRhcosISO$' -v ./pkg/isoeditor
```

This check reads `images/pxeboot/rootfs.img` from the ISO, extracts through the
native reader, and verifies the generated ramdisk's exact bytes, size, path and
mode against the reference. It rejects SquashFS inputs and fails on any shell
command. It is skipped unless the environment variables are supplied, and is
always skipped with `-short`.

# Native nmstatectl fixtures

These fixtures exercise native extraction from CPIO-wrapped EROFS and SquashFS
filesystems. Unit tests do not require erofs-utils or squashfs-tools and do not
invoke any shell commands for extraction.

| Fixture | Contents |
| --- | --- |
| `nmstatectl.erofs` | Uncompressed `usr/bin/nmstatectl`, 22 bytes, mode `0755` |
| `nmstatectl-lzma.erofs` | LZMA compact layout, 4 KiB blocks, 188416-byte binary, mode `0751` |
| `nmstatectl-lzma-64k.erofs` | LZMA full layout, 64 KiB blocks, same binary and mode |
| `nmstatectl-missing.erofs` | A regular file named `other`, with no `nmstatectl` |
| `nmstatectl-nonregular.erofs` | A directory and a symlink named `nmstatectl`, with no regular binary |

## EROFS notes

Both compressed EROFS fixtures contain `mock-nmstatectl-binary\n` repeated 8192
times, plus an earlier symlink named `nmstatectl` that extraction must skip. The
larger payload ensures the binary is actually compressed. Different source
permissions verify that the output archive always uses mode `0755`.

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

## SquashFS fixtures

| Fixture | Contents |
| --- | --- |
| `nmstatectl-zstd.squashfs` | Zstd-compressed `usr/bin/nmstatectl`, 188416 bytes, mode `0751` |
| `nmstatectl-missing.squashfs` | A regular file named `other`, with no `nmstatectl` |
| `nmstatectl-nonregular.squashfs` | A directory and a symlink named `nmstatectl`, with no regular binary |
| `nmstatectl-malformed.squashfs` | The first 512 bytes of a valid Zstd SquashFS image |

The compressed fixture contains `mock-nmstatectl-binary\n` repeated 8192 times,
plus an earlier symlink named `nmstatectl` that extraction must skip. Its source
permission is `0751` to verify that the output archive always uses `0755`.

Regenerate with squashfs-tools 4.6.1 or newer. The generator fixes ownership,
timestamps, filesystem metadata and compression:

```sh
MKSQUASHFS=/path/to/mksquashfs bash pkg/isoeditor/testdata/generate-squashfs.sh
```

SHA-256:

- `nmstatectl-zstd.squashfs`: `35320da0fb3b703c0e2ae292c98397c352ecc279aadfad55fcb4d3aee5a4d313`
- `nmstatectl-missing.squashfs`: `cdcac2aa602e74dbdfdf9f481e645462e953c596d0f492fe9a8b031f15f62762`
- `nmstatectl-nonregular.squashfs`: `facd7a60fbc258dc46f4a9bae9ea4ecbe086a50efc83050d3ba02aa7cd3c0878`
- `nmstatectl-malformed.squashfs`: `2da048aa03cadcd4ed1e1233529786a4b77717825f9a382e44c6c22d888166e9`

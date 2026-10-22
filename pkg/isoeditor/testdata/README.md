# EROFS nmstatectl fixtures

These fixtures provide filesystem contents for native nmstatectl extraction tests.
The extractor tests read an image at a known offset; CPIO member discovery and
ramdisk packaging are tested separately. Unit tests do not require erofs-utils.

| Fixture | Contents |
| --- | --- |
| `nmstatectl.erofs` | Uncompressed `usr/bin/nmstatectl`, 22 bytes, mode `0755` |
| `nmstatectl-lzma.erofs` | LZMA compact layout, 4 KiB blocks, 188416-byte binary, mode `0751` |
| `nmstatectl-lzma-64k.erofs` | LZMA full layout, 64 KiB blocks, same binary and mode |
| `nmstatectl-missing.erofs` | A regular file named `other`, with no `nmstatectl` |
| `nmstatectl-nonregular.erofs` | A directory and a symlink named `nmstatectl`, with no regular binary |

Both compressed fixtures contain `mock-nmstatectl-binary\n` repeated 8192 times,
plus an earlier symlink named `nmstatectl` that extraction must skip. The larger
payload ensures the binary is actually compressed. The unit tests use
`nmstatectl-lzma.erofs` to check reading the regular binary while skipping the
symlink, and `nmstatectl-nonregular.erofs` to check the missing-binary error.

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

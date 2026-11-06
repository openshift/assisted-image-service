package isoeditor

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cavaliercoder/go-cpio"
)

func readNmstateArchive(t *testing.T, archive, content []byte) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	gz.Multistream(false)
	reader := cpio.NewReader(gz)
	header, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != NmstatectlPathInRamdisk || header.Size != int64(len(content)) || header.FileInfo().Mode() != 0755 {
		t.Fatalf("unexpected nmstatectl header: %+v, mode %v", header, header.FileInfo().Mode())
	}
	actual, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, content) {
		t.Fatal("archive content does not match nmstatectl")
	}
	if _, err = reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected archive trailer, got %v", err)
	}
	remaining, err := io.Copy(io.Discard, io.LimitReader(gz, 4))
	if err != nil {
		t.Fatal(err)
	}
	if remaining > 3 {
		t.Fatal("unexpected data after the CPIO trailer")
	}
}

func writeRootfsArchive(t *testing.T, headers []cpio.Header, contents [][]byte) string {
	t.Helper()
	var archive bytes.Buffer
	writer := cpio.NewWriter(&archive)
	for i := range headers {
		if err := writer.WriteHeader(&headers[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(contents[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rootfs.img")
	if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func erofsRootfs(t *testing.T, fixture string) string {
	t.Helper()
	image, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	return writeRootfsArchive(t, []cpio.Header{
		{Name: "metadata", Mode: cpio.ModeRegular | 0644, Size: 3},
		{Name: "root.erofs", Mode: cpio.ModeRegular | 0644, Size: int64(len(image))},
	}, [][]byte{[]byte("abc"), image})
}

func squashfsRootfs(t *testing.T, fixture string) string {
	t.Helper()
	image, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	return writeRootfsArchive(t, []cpio.Header{
		{Name: "metadata", Mode: cpio.ModeRegular | 0644, Size: 3},
		{Name: "root.squashfs", Mode: cpio.ModeRegular | 0644, Size: int64(len(image))},
	}, [][]byte{[]byte("abc"), image})
}

func TestNativeErofsNmstateArchive(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		content []byte
	}{
		{"nmstatectl.erofs", []byte("mock-nmstatectl-binary")},
		{"nmstatectl-lzma.erofs", bytes.Repeat([]byte("mock-nmstatectl-binary\n"), 8192)},
		{"nmstatectl-lzma-64k.erofs", bytes.Repeat([]byte("mock-nmstatectl-binary\n"), 8192)},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			rootfs := erofsRootfs(t, tc.fixture)
			archive, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs)
			if err != nil {
				t.Fatal(err)
			}
			readNmstateArchive(t, archive, tc.content)
		})
	}
}

func TestErofsExtractionErrors(t *testing.T) {
	for _, fixture := range []string{"nmstatectl-missing.erofs", "nmstatectl-nonregular.erofs"} {
		t.Run(fixture, func(t *testing.T) {
			handler := NewNmstateHandler()
			if _, err := handler.BuildNmstateCpioArchive(erofsRootfs(t, fixture)); err == nil || !strings.Contains(err.Error(), "nmstatectl not found") {
				t.Fatalf("expected missing regular binary error, got %v", err)
			}
		})
	}
	t.Run("malformed EROFS", func(t *testing.T) {
		image := make([]byte, 4096)
		rootfs := writeRootfsArchive(t, []cpio.Header{{Name: "root.erofs", Mode: cpio.ModeRegular | 0644, Size: int64(len(image))}}, [][]byte{image})
		if _, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs); err == nil || !strings.Contains(err.Error(), "magic") {
			t.Fatalf("expected EROFS reader error, got %v", err)
		}
	})
	t.Run("rootfs removed after inspection", func(t *testing.T) {
		rootfs := erofsRootfs(t, "nmstatectl.erofs")
		if _, err := inspectRootfs(rootfs); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(rootfs); err != nil {
			t.Fatal(err)
		}
		if _, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected missing rootfs error, got %v", err)
		}
	})
}

func TestNativeSquashfsNmstateArchive(t *testing.T) {
	rootfs := squashfsRootfs(t, "nmstatectl-zstd.squashfs")
	archive, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs)
	if err != nil {
		t.Fatal(err)
	}
	readNmstateArchive(t, archive, bytes.Repeat([]byte("mock-nmstatectl-binary\n"), 8192))
}

func TestSquashfsExtractionErrors(t *testing.T) {
	for _, fixture := range []string{"nmstatectl-missing.squashfs", "nmstatectl-nonregular.squashfs"} {
		t.Run(fixture, func(t *testing.T) {
			handler := NewNmstateHandler()
			if _, err := handler.BuildNmstateCpioArchive(squashfsRootfs(t, fixture)); err == nil || !strings.Contains(err.Error(), "nmstatectl not found") {
				t.Fatalf("expected missing regular binary error, got %v", err)
			}
		})
	}
	t.Run("malformed SquashFS", func(t *testing.T) {
		image, err := os.ReadFile("testdata/nmstatectl-malformed.squashfs")
		if err != nil {
			t.Fatal(err)
		}
		rootfs := writeRootfsArchive(t, []cpio.Header{{Name: "root.squashfs", Mode: cpio.ModeRegular | 0644, Size: int64(len(image))}}, [][]byte{image})
		if _, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs); err == nil {
			t.Fatal("expected SquashFS reader error")
		}
	})
	t.Run("rootfs removed after inspection", func(t *testing.T) {
		rootfs := squashfsRootfs(t, "nmstatectl-zstd.squashfs")
		if _, err := inspectRootfs(rootfs); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(rootfs); err != nil {
			t.Fatal(err)
		}
		if _, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected missing rootfs error, got %v", err)
		}
	})
}

func TestInspectRootfs(t *testing.T) {
	for _, name := range []string{"root.erofs", "./root.erofs", "root.squashfs"} {
		t.Run(name, func(t *testing.T) {
			content := []byte("filesystem")
			rootfs := writeRootfsArchive(t, []cpio.Header{{Name: name, Mode: cpio.ModeRegular | 0644, Size: int64(len(content))}}, [][]byte{content})
			root, err := inspectRootfs(rootfs)
			if err != nil {
				t.Fatal(err)
			}
			if root.Extension != strings.TrimPrefix(filepath.Ext(name), ".") || root.Name != name || root.Size != int64(len(content)) {
				t.Fatalf("unexpected root member: %+v", root)
			}
			archive, err := os.ReadFile(rootfs)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(archive[root.Offset:root.Offset+root.Size], content) {
				t.Fatal("incorrect root filesystem offset")
			}
		})
	}
	for _, tc := range []struct {
		name    string
		header  cpio.Header
		content []byte
		want    string
	}{
		{"missing root", cpio.Header{Name: "metadata", Mode: cpio.ModeRegular | 0644, Size: 3}, []byte("abc"), "failed to find root file"},
		{"directory root", cpio.Header{Name: "root.erofs", Mode: cpio.ModeDir | 0755}, nil, "not a regular file"},
		{"symlink root", cpio.Header{Name: "root.erofs", Mode: cpio.ModeSymlink | 0777, Size: 5}, []byte("other"), "not a regular file"},
		{"empty root", cpio.Header{Name: "root.erofs", Mode: cpio.ModeRegular | 0644}, nil, "invalid size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rootfs := writeRootfsArchive(t, []cpio.Header{tc.header}, [][]byte{tc.content})
			if _, err := inspectRootfs(rootfs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	t.Run("unsupported filesystem", func(t *testing.T) {
		rootfs := writeRootfsArchive(t, []cpio.Header{{Name: "root.ext4", Mode: cpio.ModeRegular | 0644, Size: 3}}, [][]byte{[]byte("abc")})
		if _, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs); err == nil || !strings.Contains(err.Error(), "unknown extension") {
			t.Fatalf("expected unsupported filesystem error, got %v", err)
		}
	})
	t.Run("malformed CPIO", func(t *testing.T) {
		rootfs := filepath.Join(t.TempDir(), "rootfs.img")
		if err := os.WriteFile(rootfs, []byte("not a CPIO archive"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := inspectRootfs(rootfs); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("expected CPIO parsing error, got %v", err)
		}
	})
	t.Run("missing rootfs", func(t *testing.T) {
		if _, err := inspectRootfs(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected missing rootfs error, got %v", err)
		}
	})
	for _, tc := range []struct {
		name     string
		rootName string
		fixture  string
	}{
		{"squashfs member extends beyond EOF", "root.squashfs", "nmstatectl-zstd.squashfs"},
		{"erofs member extends beyond EOF", "root.erofs", "nmstatectl.erofs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			writer := cpio.NewWriter(&archive)
			if err := writer.WriteHeader(&cpio.Header{Name: tc.rootName, Mode: cpio.ModeRegular | 0644, Size: int64(2 * len(image))}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(image); err != nil {
				t.Fatal(err)
			}
			rootfs := filepath.Join(t.TempDir(), "rootfs.img")
			if err := os.WriteFile(rootfs, archive.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewNmstateHandler().BuildNmstateCpioArchive(rootfs); err == nil || !strings.Contains(err.Error(), "extends beyond rootfs.img") {
				t.Fatalf("expected truncated member error, got %v", err)
			}
		})
	}
}

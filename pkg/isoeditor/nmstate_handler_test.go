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
	"go.uber.org/mock/gomock"
)

func readNmstateArchive(t *testing.T, archive, content []byte, mode fs.FileMode) {
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
	if header.Name != NmstatectlPathInRamdisk || header.Size != int64(len(content)) || header.FileInfo().Mode() != mode {
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

func TestBuildNmstateCpioArchive(t *testing.T) {
	for _, mode := range []fs.FileMode{0751, fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0755} {
		t.Run(mode.String(), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory := NewMockNmstatectlExtractorFactory(ctrl)
			extractor := NewMockNmstatectlExtractor(ctrl)
			content := []byte("mock-nmstatectl-binary")
			workDir := filepath.Join(t.TempDir(), "absent")
			factory.EXPECT().CreateNmstatectlExtractor("rootfs.img").Return(extractor, nil)
			extractor.EXPECT().ExtractNmstatectl("rootfs.img", workDir).Return(&NmstatectlArtifact{Content: content, Mode: mode}, nil)
			archive, err := NewNmstateHandler(workDir, NewMockExecuter(ctrl), factory).BuildNmstateCpioArchive("rootfs.img")
			if err != nil {
				t.Fatal(err)
			}
			readNmstateArchive(t, archive, content, mode)
		})
	}
	t.Run("factory error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := NewMockNmstatectlExtractorFactory(ctrl)
		expected := errors.New("factory failed")
		factory.EXPECT().CreateNmstatectlExtractor("rootfs.img").Return(nil, expected)
		_, err := NewNmstateHandler(t.TempDir(), NewMockExecuter(ctrl), factory).BuildNmstateCpioArchive("rootfs.img")
		if !errors.Is(err, expected) {
			t.Fatalf("expected factory error, got %v", err)
		}
	})
	t.Run("extraction error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := NewMockNmstatectlExtractorFactory(ctrl)
		extractor := NewMockNmstatectlExtractor(ctrl)
		expected := errors.New("extraction failed")
		factory.EXPECT().CreateNmstatectlExtractor("rootfs.img").Return(extractor, nil)
		extractor.EXPECT().ExtractNmstatectl("rootfs.img", gomock.Any()).Return(nil, expected)
		_, err := NewNmstateHandler(t.TempDir(), NewMockExecuter(ctrl), factory).BuildNmstateCpioArchive("rootfs.img")
		if !errors.Is(err, expected) {
			t.Fatalf("expected extraction error, got %v", err)
		}
	})
}

func TestNativeErofsNmstateArchive(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		content []byte
		mode    fs.FileMode
	}{
		{"nmstatectl.erofs", []byte("mock-nmstatectl-binary"), 0755},
		{"nmstatectl-lzma.erofs", bytes.Repeat([]byte("mock-nmstatectl-binary\n"), 8192), 0751},
		{"nmstatectl-lzma-64k.erofs", bytes.Repeat([]byte("mock-nmstatectl-binary\n"), 8192), 0751},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			rootfs := erofsRootfs(t, tc.fixture)
			// Any shell command is an unexpected mock call and fails the test.
			executer := NewMockExecuter(gomock.NewController(t))
			workDir := filepath.Join(t.TempDir(), "absent")
			archive, err := NewNmstateHandler(workDir, executer, NewNmstatectlExtractorFactory(executer)).BuildNmstateCpioArchive(rootfs)
			if err != nil {
				t.Fatal(err)
			}
			readNmstateArchive(t, archive, tc.content, tc.mode)
			if _, err := os.Stat(workDir); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("native extraction created a temporary directory: %v", err)
			}
		})
	}
}

func TestErofsExtractionErrors(t *testing.T) {
	for _, fixture := range []string{"nmstatectl-missing.erofs", "nmstatectl-nonregular.erofs"} {
		t.Run(fixture, func(t *testing.T) {
			executer := NewMockExecuter(gomock.NewController(t))
			handler := NewNmstateHandler(t.TempDir(), executer, NewNmstatectlExtractorFactory(executer))
			if _, err := handler.BuildNmstateCpioArchive(erofsRootfs(t, fixture)); err == nil || !strings.Contains(err.Error(), "nmstatectl not found") {
				t.Fatalf("expected missing regular binary error, got %v", err)
			}
		})
	}
	t.Run("malformed EROFS", func(t *testing.T) {
		image := make([]byte, 4096)
		rootfs := writeRootfsArchive(t, []cpio.Header{{Name: "root.erofs", Mode: cpio.ModeRegular | 0644, Size: int64(len(image))}}, [][]byte{image})
		executer := NewMockExecuter(gomock.NewController(t))
		if _, err := NewNmstateHandler(t.TempDir(), executer, NewNmstatectlExtractorFactory(executer)).BuildNmstateCpioArchive(rootfs); err == nil || !strings.Contains(err.Error(), "magic") {
			t.Fatalf("expected EROFS reader error, got %v", err)
		}
	})
	t.Run("rootfs removed after inspection", func(t *testing.T) {
		rootfs := erofsRootfs(t, "nmstatectl.erofs")
		extractor, err := NewNmstatectlExtractorFactory(nil).CreateNmstatectlExtractor(rootfs)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(rootfs); err != nil {
			t.Fatal(err)
		}
		if _, err := extractor.ExtractNmstatectl(rootfs, t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected missing rootfs error, got %v", err)
		}
	})
}

func TestInspectRootfs(t *testing.T) {
	for _, name := range []string{"root.erofs", "./root.erofs", "root.squashfs"} {
		t.Run(name, func(t *testing.T) {
			content := []byte("filesystem")
			rootfs := writeRootfsArchive(t, []cpio.Header{{Name: name, Mode: cpio.ModeRegular | 0644, Size: int64(len(content))}}, [][]byte{content})
			extension, offset, size, err := inspectRootfs(rootfs)
			if err != nil {
				t.Fatal(err)
			}
			if extension != strings.TrimPrefix(filepath.Ext(name), ".") || size != int64(len(content)) {
				t.Fatalf("unexpected root member: %s %d", extension, size)
			}
			archive, err := os.ReadFile(rootfs)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(archive[offset:offset+size], content) {
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
			if _, err := NewNmstatectlExtractorFactory(nil).CreateNmstatectlExtractor(rootfs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	t.Run("unsupported filesystem", func(t *testing.T) {
		rootfs := writeRootfsArchive(t, []cpio.Header{{Name: "root.ext4", Mode: cpio.ModeRegular | 0644, Size: 3}}, [][]byte{[]byte("abc")})
		if _, err := NewNmstatectlExtractorFactory(nil).CreateNmstatectlExtractor(rootfs); err == nil || !strings.Contains(err.Error(), "unknown extension") {
			t.Fatalf("expected unsupported filesystem error, got %v", err)
		}
	})
	t.Run("malformed CPIO", func(t *testing.T) {
		rootfs := filepath.Join(t.TempDir(), "rootfs.img")
		if err := os.WriteFile(rootfs, []byte("not a CPIO archive"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := inspectRootfs(rootfs); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("expected CPIO parsing error, got %v", err)
		}
	})
	t.Run("missing rootfs", func(t *testing.T) {
		if _, _, _, err := inspectRootfs(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected missing rootfs error, got %v", err)
		}
	})
	t.Run("member extends beyond EOF", func(t *testing.T) {
		image, err := os.ReadFile("testdata/nmstatectl.erofs")
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		writer := cpio.NewWriter(&archive)
		if err := writer.WriteHeader(&cpio.Header{Name: "root.erofs", Mode: cpio.ModeRegular | 0644, Size: int64(2 * len(image))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(image); err != nil {
			t.Fatal(err)
		}
		rootfs := filepath.Join(t.TempDir(), "rootfs.img")
		if err := os.WriteFile(rootfs, archive.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		executer := NewMockExecuter(gomock.NewController(t))
		if _, err := NewNmstateHandler(t.TempDir(), executer, NewNmstatectlExtractorFactory(executer)).BuildNmstateCpioArchive(rootfs); err == nil || !strings.Contains(err.Error(), "extends beyond rootfs.img") {
			t.Fatalf("expected truncated member error, got %v", err)
		}
	})
}

func TestSquashfsNmstateExtraction(t *testing.T) {
	for _, failure := range []string{"success", "cpio", "listing", "missing binary", "extraction"} {
		t.Run(failure, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			executer := NewMockExecuter(ctrl)
			rootfs := filepath.Join(t.TempDir(), "rootfs.img")
			workDir := t.TempDir()
			var extractionDir string
			expected := errors.New(failure + " failed")
			executer.EXPECT().Execute("cat "+rootfs+" | cpio -i", gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				extractionDir = dir
				if failure == "cpio" {
					return "", expected
				}
				return "", nil
			})
			binaryPath := "/ostree/deploy/rhcos/deploy/abc123/usr/bin/nmstatectl"
			content := []byte("mock-nmstatectl-binary")
			if failure != "cpio" {
				executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -dest '' -lc root.squashfs", gomock.Any()).DoAndReturn(func(_ string, _ string) (string, error) {
					switch failure {
					case "listing":
						return "", expected
					case "missing binary":
						return "/usr/bin/other", nil
					default:
						return binaryPath, nil
					}
				})
			}
			if failure == "success" || failure == "extraction" {
				executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -no-xattrs root.squashfs -extract-file "+binaryPath, gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
					if failure == "extraction" {
						return "", expected
					}
					path := filepath.Join(dir, "squashfs-root", binaryPath)
					if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, content, 0600); err != nil {
						return "", err
					}
					return "", os.Chmod(path, 0751)
				})
			}
			factory := NewMockNmstatectlExtractorFactory(ctrl)
			factory.EXPECT().CreateNmstatectlExtractor(rootfs).Return(&squashfsExtractor{executer: executer}, nil)
			archive, err := NewNmstateHandler(workDir, executer, factory).BuildNmstateCpioArchive(rootfs)
			if failure == "success" {
				if err != nil {
					t.Fatal(err)
				}
				readNmstateArchive(t, archive, content, 0751)
			} else if failure == "missing binary" {
				if err == nil || !strings.Contains(err.Error(), "nmstatectl not found") {
					t.Fatalf("expected missing binary error, got %v", err)
				}
			} else if !errors.Is(err, expected) {
				t.Fatalf("expected command error, got %v", err)
			}
			if _, err := os.Stat(extractionDir); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("temporary extraction directory was not removed: %v", err)
			}
		})
	}
}

package isoeditor

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cavaliercoder/go-cpio"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

func writeRootfsArchive(workDir string, headers []cpio.Header, contents [][]byte) string {
	var archive bytes.Buffer
	writer := cpio.NewWriter(&archive)
	for i := range headers {
		Expect(writer.WriteHeader(&headers[i])).To(Succeed())
		_, err := writer.Write(contents[i])
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(writer.Close()).To(Succeed())
	path := filepath.Join(workDir, "rootfs.img")
	Expect(os.WriteFile(path, archive.Bytes(), 0600)).To(Succeed())
	return path
}

func writeErofsRootfs(workDir, fixture string) (string, *erofsExtractor) {
	image, err := os.ReadFile(filepath.Join("testdata", fixture))
	Expect(err).NotTo(HaveOccurred())
	padding := make([]byte, 128)
	rootfs, err := os.CreateTemp(workDir, "rootfs-*.img")
	Expect(err).NotTo(HaveOccurred())
	defer rootfs.Close()
	_, err = rootfs.Write(append(padding, image...))
	Expect(err).NotTo(HaveOccurred())
	return rootfs.Name(), &erofsExtractor{offset: int64(len(padding)), size: int64(len(image))}
}

var _ = Describe("Nmstate handler", func() {
	var (
		tempDir  string
		workDir  string
		ctrl     *gomock.Controller
		executer *MockExecuter
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "nmstate-test-")
		Expect(err).NotTo(HaveOccurred())
		workDir = filepath.Join(tempDir, "absent")
		ctrl = gomock.NewController(GinkgoT())
		executer = NewMockExecuter(ctrl)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Describe("BuildNmstateCpioArchive", func() {
		var (
			factory   *MockNmstatectlExtractorFactory
			extractor *MockNmstatectlExtractor
			handler   NmstateHandler
		)

		BeforeEach(func() {
			factory = NewMockNmstatectlExtractorFactory(ctrl)
			extractor = NewMockNmstatectlExtractor(ctrl)
			handler = NewNmstateHandler(workDir, executer, factory)
		})

		It("packages the extracted binary with executable permissions at the ramdisk path", func() {
			content := []byte("mock-nmstatectl-binary")
			factory.EXPECT().CreateNmstatectlExtractor("rootfs.img").Return(extractor, nil)
			extractor.EXPECT().ExtractNmstatectl("rootfs.img", workDir).Return(content, nil)

			archive, err := handler.BuildNmstateCpioArchive("rootfs.img")
			Expect(err).NotTo(HaveOccurred())
			gz, err := gzip.NewReader(bytes.NewReader(archive))
			Expect(err).NotTo(HaveOccurred())
			defer gz.Close()
			reader := cpio.NewReader(gz)
			header, err := reader.Next()
			Expect(err).NotTo(HaveOccurred())
			Expect(header.Name).To(Equal(NmstatectlPathInRamdisk))
			Expect(header.FileInfo().Mode()).To(Equal(fs.FileMode(0755)))
			actual, err := io.ReadAll(reader)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(content))
		})

		It("returns the factory error when creating an extractor fails", func() {
			expected := errors.New("factory failed")
			factory.EXPECT().CreateNmstatectlExtractor("rootfs.img").Return(nil, expected)

			_, err := handler.BuildNmstateCpioArchive("rootfs.img")
			Expect(err).To(MatchError(expected))
		})

		It("returns the extraction error when reading nmstatectl fails", func() {
			expected := errors.New("extraction failed")
			factory.EXPECT().CreateNmstatectlExtractor("rootfs.img").Return(extractor, nil)
			extractor.EXPECT().ExtractNmstatectl("rootfs.img", workDir).Return(nil, expected)

			_, err := handler.BuildNmstateCpioArchive("rootfs.img")
			Expect(err).To(MatchError(expected))
		})
	})

	Describe("CreateNmstatectlExtractor", func() {
		It("selects an EROFS extractor with the root member's offset and size", func() {
			content := []byte("filesystem")
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{
				{Name: "metadata", Mode: cpio.ModeRegular | 0644, Size: 3},
				{Name: "root.erofs", Mode: cpio.ModeRegular | 0644, Size: int64(len(content))},
			}, [][]byte{[]byte("abc"), content})
			archive, err := os.ReadFile(rootfs)
			Expect(err).NotTo(HaveOccurred())

			extractor, err := NewNmstatectlExtractorFactory(executer).CreateNmstatectlExtractor(rootfs)
			Expect(err).NotTo(HaveOccurred())
			Expect(extractor).To(Equal(&erofsExtractor{offset: int64(bytes.Index(archive, content)), size: int64(len(content))}))
		})

		It("selects a SquashFS extractor with the supplied executer", func() {
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{{Name: "root.squashfs", Mode: cpio.ModeRegular | 0644, Size: 3}}, [][]byte{[]byte("abc")})

			extractor, err := NewNmstatectlExtractorFactory(executer).CreateNmstatectlExtractor(rootfs)
			Expect(err).NotTo(HaveOccurred())
			Expect(extractor).To(Equal(&squashfsExtractor{executer: executer}))
		})

		It("rejects unsupported root filesystem extensions", func() {
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{{Name: "root.ext4", Mode: cpio.ModeRegular | 0644, Size: 3}}, [][]byte{[]byte("abc")})

			_, err := NewNmstatectlExtractorFactory(executer).CreateNmstatectlExtractor(rootfs)
			Expect(err).To(MatchError(ContainSubstring("unknown extension")))
		})
	})

	Describe("erofsExtractor.ExtractNmstatectl", func() {
		It("reads the regular nmstatectl from the embedded filesystem, skipping an earlier symlink", func() {
			rootfs, extractor := writeErofsRootfs(tempDir, "nmstatectl-lzma.erofs")

			content, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(bytes.Repeat([]byte("mock-nmstatectl-binary\n"), 8192)))
		})

		It("reports a missing binary when no regular nmstatectl exists", func() {
			rootfs, extractor := writeErofsRootfs(tempDir, "nmstatectl-nonregular.erofs")

			_, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).To(MatchError(ContainSubstring("nmstatectl not found")))
		})
	})

	Describe("inspectRootfs", func() {
		It("skips metadata and finds the extension and data bounds of a ./root.erofs member", func() {
			content := []byte("filesystem")
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{
				{Name: "metadata", Mode: cpio.ModeRegular | 0644, Size: 3},
				{Name: "./root.erofs", Mode: cpio.ModeRegular | 0644, Size: int64(len(content))},
			}, [][]byte{[]byte("abc"), content})

			extension, offset, size, err := inspectRootfs(rootfs)
			Expect(err).NotTo(HaveOccurred())
			Expect(extension).To(Equal("erofs"))
			Expect(size).To(Equal(int64(len(content))))
			archive, err := os.ReadFile(rootfs)
			Expect(err).NotTo(HaveOccurred())
			Expect(archive[offset : offset+size]).To(Equal(content))
		})

		It("reports a missing root member when the archive only contains metadata", func() {
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{{Name: "metadata", Mode: cpio.ModeRegular | 0644, Size: 3}}, [][]byte{[]byte("abc")})

			_, _, _, err := inspectRootfs(rootfs)
			Expect(err).To(MatchError(ContainSubstring("failed to find root file")))
		})

		It("rejects a root member that is not a regular file", func() {
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{{Name: "root.erofs", Mode: cpio.ModeDir | 0755}}, [][]byte{nil})

			_, _, _, err := inspectRootfs(rootfs)
			Expect(err).To(MatchError(ContainSubstring("not a regular file")))
		})

		It("rejects an empty root member", func() {
			rootfs := writeRootfsArchive(tempDir, []cpio.Header{{Name: "root.erofs", Mode: cpio.ModeRegular | 0644}}, [][]byte{nil})

			_, _, _, err := inspectRootfs(rootfs)
			Expect(err).To(MatchError(ContainSubstring("invalid size")))
		})

		It("rejects a root member whose declared size extends beyond the archive", func() {
			var archive bytes.Buffer
			writer := cpio.NewWriter(&archive)
			Expect(writer.WriteHeader(&cpio.Header{Name: "root.erofs", Mode: cpio.ModeRegular | 0644, Size: 4096})).To(Succeed())
			rootfs := filepath.Join(tempDir, "rootfs.img")
			Expect(os.WriteFile(rootfs, archive.Bytes(), 0600)).To(Succeed())

			_, _, _, err := inspectRootfs(rootfs)
			Expect(err).To(MatchError(ContainSubstring("extends beyond rootfs.img")))
		})
	})

	Describe("squashfsExtractor.ExtractNmstatectl", func() {
		const binaryPath = "/ostree/deploy/rhcos/deploy/abc123/usr/bin/nmstatectl"
		var (
			rootfs        string
			extractionDir string
			extractor     NmstatectlExtractor
		)

		BeforeEach(func() {
			rootfs = filepath.Join(tempDir, "rootfs.img")
			extractionDir = ""
			extractor = &squashfsExtractor{executer: executer}
		})

		AfterEach(func() {
			Expect(extractionDir).NotTo(BeEmpty())
			_, err := os.Stat(extractionDir)
			Expect(err).To(MatchError(fs.ErrNotExist), "temporary extraction directory was not removed")
		})

		It("returns the extracted binary and removes the temporary extraction directory", func() {
			content := []byte("mock-nmstatectl-binary")
			executer.EXPECT().Execute("cat "+rootfs+" | cpio -i", gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				extractionDir = dir
				return "", nil
			})
			executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -dest '' -lc root.squashfs", gomock.Any()).Return(binaryPath, nil)
			executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -no-xattrs root.squashfs -extract-file "+binaryPath, gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				path := filepath.Join(dir, "squashfs-root", binaryPath)
				Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
				Expect(os.WriteFile(path, content, 0600)).To(Succeed())
				return "", nil
			})

			actual, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(content))
		})

		It("returns the CPIO command error and removes the temporary extraction directory", func() {
			expected := errors.New("cpio failed")
			executer.EXPECT().Execute("cat "+rootfs+" | cpio -i", gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				extractionDir = dir
				return "", expected
			})

			_, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).To(MatchError(expected))
		})

		It("returns the SquashFS listing error and removes the temporary extraction directory", func() {
			expected := errors.New("listing failed")
			executer.EXPECT().Execute("cat "+rootfs+" | cpio -i", gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				extractionDir = dir
				return "", nil
			})
			executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -dest '' -lc root.squashfs", gomock.Any()).Return("", expected)

			_, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).To(MatchError(expected))
		})

		It("reports a missing binary when the listing has no nmstatectl", func() {
			executer.EXPECT().Execute("cat "+rootfs+" | cpio -i", gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				extractionDir = dir
				return "", nil
			})
			executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -dest '' -lc root.squashfs", gomock.Any()).Return("/usr/bin/other", nil)

			_, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).To(MatchError(ContainSubstring("nmstatectl not found")))
		})

		It("returns the SquashFS extraction error and removes the temporary extraction directory", func() {
			expected := errors.New("extraction failed")
			executer.EXPECT().Execute("cat "+rootfs+" | cpio -i", gomock.Any()).DoAndReturn(func(_ string, dir string) (string, error) {
				extractionDir = dir
				return "", nil
			})
			executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -dest '' -lc root.squashfs", gomock.Any()).Return(binaryPath, nil)
			executer.EXPECT().Execute("ulimit -n 1024 ; unsquashfs -no-xattrs root.squashfs -extract-file "+binaryPath, gomock.Any()).Return("", expected)

			_, err := extractor.ExtractNmstatectl(rootfs, workDir)
			Expect(err).To(MatchError(expected))
		})
	})
})

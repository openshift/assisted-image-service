package isoeditor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/mock/gomock"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Context("with test files", func() {
	var (
		isoFile  string
		filesDir string
		workDir  string
		volumeID = "Assisted123"
	)

	BeforeEach(func() {
		filesDir, isoFile = createTestFiles(volumeID)

		var err error
		workDir, err = os.MkdirTemp("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
		Expect(os.RemoveAll(workDir)).To(Succeed())
	})

	Describe("BuildNmstateCpioArchive", func() {
		var (
			rootfsPath                     string
			newNmstateHandler              NmstateHandler
			ctrl                           *gomock.Controller
			mockExecuter                   *MockExecuter
			mockNmstatectlExtractorFactory *MockNmstatectlExtractorFactory
		)

		BeforeEach(func() {
			rootfsPath = filepath.Join(workDir, "rootfs.img")

			// Create a dummy rootfs file
			err := os.WriteFile(rootfsPath, []byte("dummy-rootfs"), 0644) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())

			ctrl = gomock.NewController(GinkgoT())
			mockExecuter = NewMockExecuter(ctrl)
			mockNmstatectlExtractorFactory = NewMockNmstatectlExtractorFactory(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("handles cpio extraction failure", func() {
			// Mock cpio extraction failure
			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("cpio extraction failed"))

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

			_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cpio extraction failed"))
		})

		It("handles nmstate extractor creation failure", func() {
			gomock.InOrder(
				// First call succeeds (cpio extraction)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				// Second call: create nmstate extractor (error returned)
				mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(nil, errors.New("failed to create nmstate extractor")),
			)

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

			_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create nmstate extractor"))
		})

		Context("squashfs", func() {
			It("builds nmstate cpio archive", func() {
				// Mock the execution sequence for successful extraction
				gomock.InOrder(
					// First call: extract rootfs with cpio
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (squashfs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&squashfsExtractor{executer: mockExecuter}, nil),
					// Third call: list squashfs contents to find nmstatectl
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("/ostree/deploy/rhcos/deploy/abc123/usr/bin/nmstatectl", nil),
					// Fourth call: extract the nmstatectl binary
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				)

				// Create the expected directory structure and nmstatectl binary
				extractDir := filepath.Join(workDir, "nmstate")
				squashfsRoot := filepath.Join(extractDir, "squashfs-root")
				nmstatectlDir := filepath.Join(squashfsRoot, "ostree", "deploy", "rhcos", "deploy", "abc123", "usr", "bin")
				err := os.MkdirAll(nmstatectlDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				nmstatectlBinary := filepath.Join(nmstatectlDir, "nmstatectl")
				nmstatectlContent := []byte("mock-nmstatectl-binary")
				err = os.WriteFile(nmstatectlBinary, nmstatectlContent, 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(compressedCpio).NotTo(BeEmpty())
			})

			It("handles squashfs listing failure", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (squashfs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&squashfsExtractor{executer: mockExecuter}, nil),
					// Third call fails (squashfs listing)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("squashfs listing failed")),
				)

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("squashfs listing failed"))
			})

			It("handles nmstatectl binary not found", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (squashfs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&squashfsExtractor{executer: mockExecuter}, nil),
					// Third call succeeds but doesn't return nmstatectl path
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("/some/other/binary", nil),
					// Fourth call should still be made with empty binary path
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				)

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no such file or directory"))
			})

			It("handles squashfs extraction failure", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (squashfs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&squashfsExtractor{executer: mockExecuter}, nil),
					// Third call succeeds (squashfs listing)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("/ostree/deploy/rhcos/deploy/abc123/usr/bin/nmstatectl", nil),
					// Fourth call fails (squashfs extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("squashfs extraction failed")),
				)

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("squashfs extraction failed"))
			})
		})

		Context("erofs", func() {
			It("builds nmstate cpio archive with nested directory structure", func() {
				// Simplified: nmstatectl is in /usr/bin directly
				rootDirOutput := `Path : /
       NID TYPE  FILENAME
        37    2  .
        37    2  ..
        44    1  .aleph-version.json
        51    2  usr`

				usrDirOutput := `Path : /usr
       NID TYPE  FILENAME
       110    2  .
       100    2  ..
       130    2  bin`

				binDirOutput := `Path : /usr/bin
       NID TYPE  FILENAME
       130    2  .
       110    2  ..
       150    1  bash
       170    1  nmstatectl`

				gomock.InOrder(
					// First call: extract rootfs with cpio
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (erofs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					// Recursive directory listing calls
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/ --ls root.erofs"), gomock.Any()).Return(rootDirOutput, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/usr --ls root.erofs"), gomock.Any()).Return(usrDirOutput, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/usr/bin --ls root.erofs"), gomock.Any()).Return(binDirOutput, nil),
					// Extract the nmstatectl binary
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --cat --path=/usr/bin/nmstatectl root.erofs > nmstatectl"), gomock.Any()).Return("", nil),
				)

				// Create the expected directory structure
				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				// Create the extracted nmstatectl binary (simulating dump.erofs output)
				nmstatectlBinary := filepath.Join(extractDir, "nmstatectl")
				nmstatectlContent := []byte("mock-nmstatectl-binary")
				err = os.WriteFile(nmstatectlBinary, nmstatectlContent, 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(compressedCpio).NotTo(BeEmpty())
			})

			It("parses dump.erofs output with varying whitespace", func() {
				// Test with different amounts of whitespace in dump.erofs output
				// nmstatectl is in root directory (no subdirs to simplify the test)
				dumpOutput := `Path : /
Size: 100  On-disk size: 100  directory

       NID TYPE  FILENAME
       123    1  file-with-spaces
        10    1  nmstatectl
       999    7  symlink`

				gomock.InOrder(
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/ --ls root.erofs"), gomock.Any()).Return(dumpOutput, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --cat --path=/nmstatectl root.erofs > nmstatectl"), gomock.Any()).Return("", nil),
				)

				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				nmstatectlBinary := filepath.Join(extractDir, "nmstatectl")
				err = os.WriteFile(nmstatectlBinary, []byte("mock-binary"), 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(compressedCpio).NotTo(BeEmpty())
			})

			It("handles dump.erofs listing failure", func() {
				gomock.InOrder(
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/ --ls root.erofs"), gomock.Any()).Return("", errors.New("dump.erofs failed")),
				)

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("dump.erofs failed"))
			})

			It("handles nmstatectl not found in erofs", func() {
				// Output without nmstatectl (no subdirectories to simplify test)
				dumpOutput := `Path : /
Size: 100  On-disk size: 100  directory

       NID TYPE  FILENAME
        37    2  .
        37    2  ..
        44    1  other-file
        45    1  another-file`

				gomock.InOrder(
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/ --ls root.erofs"), gomock.Any()).Return(dumpOutput, nil),
				)

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("nmstatectl not found in root.erofs"))
			})

			It("handles dump.erofs extraction failure", func() {
				dumpOutput := `Path : /bin
Size: 100  On-disk size: 100  directory

       NID TYPE  FILENAME
       130    2  .
       110    2  ..
       170    1  nmstatectl`

				gomock.InOrder(
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/ --ls root.erofs"), gomock.Any()).Return(dumpOutput, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --cat --path=/nmstatectl root.erofs > nmstatectl"), gomock.Any()).Return("", errors.New("extraction failed")),
				)

				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				rootErofsPath := filepath.Join(extractDir, "root.erofs")
				err = os.WriteFile(rootErofsPath, []byte("dummy-erofs"), 0644) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err = newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("extraction failed"))
			})

			It("skips directories with failed listing and continues search", func() {
				rootOutput := `Path : /
       NID TYPE  FILENAME
        37    2  .
        37    2  ..
        51    2  bad-dir
        63    2  good-dir`

				goodDirOutput := `Path : /good-dir
       NID TYPE  FILENAME
        63    2  .
        37    2  ..
        70    1  nmstatectl`

				gomock.InOrder(
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/ --ls root.erofs"), gomock.Any()).Return(rootOutput, nil),
					// bad-dir fails
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/bad-dir --ls root.erofs"), gomock.Any()).Return("", errors.New("permission denied")),
					// good-dir succeeds
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --path=/good-dir --ls root.erofs"), gomock.Any()).Return(goodDirOutput, nil),
					mockExecuter.EXPECT().Execute(gomock.Eq("dump.erofs --cat --path=/good-dir/nmstatectl root.erofs > nmstatectl"), gomock.Any()).Return("", nil),
				)

				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				rootErofsPath := filepath.Join(extractDir, "root.erofs")
				err = os.WriteFile(rootErofsPath, []byte("dummy-erofs"), 0644) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				nmstatectlBinary := filepath.Join(extractDir, "nmstatectl")
				err = os.WriteFile(nmstatectlBinary, []byte("mock-binary"), 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(compressedCpio).NotTo(BeEmpty())
			})
		})
	})
})

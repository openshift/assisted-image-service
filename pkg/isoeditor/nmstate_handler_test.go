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
			var extractorDir string

			BeforeEach(func() {
				var err error
				extractorDir, err = os.MkdirTemp("", "erofsExtractor")
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				Expect(os.RemoveAll(extractorDir)).To(Succeed())
			})

			copyFixture := func(name string) {
				source, err := os.ReadFile(filepath.Join("testdata", name))
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(extractorDir, "root.erofs"), source, 0644)).To(Succeed())
			}

			It("extracts nmstatectl from an uncompressed EROFS image", func() {
				copyFixture("nmstatectl.erofs")

				extractor := &erofsExtractor{}
				path, err := extractor.ExtractNmstatectl(extractorDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(path).To(Equal("nmstatectl"))

				content, err := os.ReadFile(filepath.Join(extractorDir, path))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal([]byte("mock-nmstatectl-binary")))

				info, err := os.Stat(filepath.Join(extractorDir, path))
				Expect(err).NotTo(HaveOccurred())
				Expect(info.Mode().Perm()).To(Equal(os.FileMode(0755)))
			})

			It("extracts nmstatectl from an LZMA-compressed EROFS image", func() {
				copyFixture("nmstatectl-lzma.erofs")

				extractor := &erofsExtractor{}
				path, err := extractor.ExtractNmstatectl(extractorDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(path).To(Equal("nmstatectl"))

				content, err := os.ReadFile(filepath.Join(extractorDir, path))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal([]byte("mock-nmstatectl-binary")))
			})

			It("returns an error when root.erofs is missing", func() {
				extractor := &erofsExtractor{}
				_, err := extractor.ExtractNmstatectl(extractorDir)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to open root.erofs"))
			})

			It("builds the nmstate archive using the native EROFS reader", func() {
				extractDir := filepath.Join(workDir, "nmstate")
				Expect(os.MkdirAll(extractDir, os.ModePerm)).To(Succeed())

				fixture, err := os.ReadFile(filepath.Join("testdata", "nmstatectl-lzma.erofs"))
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(extractDir, "root.erofs"), fixture, 0644)).To(Succeed())

				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil)
				mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{}, nil)

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)
				compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(compressedCpio).NotTo(BeEmpty())
			})
		})

	})
})

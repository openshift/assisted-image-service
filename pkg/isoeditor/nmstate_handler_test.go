package isoeditor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"

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
			It("builds nmstate cpio archive", func() {
				// Mock the execution sequence for successful extraction
				gomock.InOrder(
					// First call: extract rootfs with cpio
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (erofs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					// Third call: extract the nmstatectl binary using dump.erofs
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				)

				// Create the expected directory structure
				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				// Create a minimal erofs file with nmstatectl using mkfs.erofs
				erofsSourceDir, err := os.MkdirTemp("", "erofs-source")
				Expect(err).NotTo(HaveOccurred())
				defer os.RemoveAll(erofsSourceDir)

				nmstatectlPath := filepath.Join(erofsSourceDir, "nmstatectl")
				nmstatectlContent := []byte("mock-nmstatectl-binary")
				err = os.WriteFile(nmstatectlPath, nmstatectlContent, 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				// Create erofs file using mkfs.erofs
				rootErofsPath := filepath.Join(extractDir, "root.erofs")
				cmd := exec.Command("mkfs.erofs", rootErofsPath, erofsSourceDir)
				err = cmd.Run()
				if err != nil {
					Skip("mkfs.erofs not available, skipping erofs test")
				}

				// Create the extracted nmstatectl binary (simulating dump.erofs output)
				nmstatectlBinary := filepath.Join(extractDir, "nmstatectl")
				err = os.WriteFile(nmstatectlBinary, nmstatectlContent, 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(compressedCpio).NotTo(BeEmpty())
			})

			It("handles root.erofs file open failure", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (erofs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
				)

				// Don't create root.erofs file to trigger open failure
				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				// os.Open returns "no such file or directory" error
				Expect(err.Error()).To(ContainSubstring("no such file or directory"))
			})

			It("handles invalid erofs file", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (erofs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
				)

				// Create an invalid erofs file
				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				rootErofsPath := filepath.Join(extractDir, "root.erofs")
				err = os.WriteFile(rootErofsPath, []byte("invalid-erofs-content"), 0644) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err = newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				// erofs.EroFS will return an error when parsing invalid content
			})

			It("handles nmstatectl not found in erofs", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (erofs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
				)

				// Create an erofs file without nmstatectl
				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				erofsSourceDir, err := os.MkdirTemp("", "erofs-source")
				Expect(err).NotTo(HaveOccurred())
				defer os.RemoveAll(erofsSourceDir)

				// Create a file that's not nmstatectl
				otherFile := filepath.Join(erofsSourceDir, "other-binary")
				err = os.WriteFile(otherFile, []byte("other-binary"), 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				rootErofsPath := filepath.Join(extractDir, "root.erofs")
				cmd := exec.Command("mkfs.erofs", rootErofsPath, erofsSourceDir)
				err = cmd.Run()
				if err != nil {
					Skip("mkfs.erofs not available, skipping erofs test")
				}

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err = newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("nmstatectl not found in root.erofs"))
			})

			It("handles dump.erofs extraction failure", func() {
				gomock.InOrder(
					// First call succeeds (cpio extraction)
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
					// Second call: create nmstate extractor (erofs returned)
					mockNmstatectlExtractorFactory.EXPECT().CreateNmstatectlExtractor(gomock.Any()).Return(&erofsExtractor{executer: mockExecuter}, nil),
					// Third call: dump.erofs extraction fails
					mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", errors.New("nmstatectl extraction from erofs failed")),
				)

				// Create an erofs file with nmstatectl
				extractDir := filepath.Join(workDir, "nmstate")
				err := os.MkdirAll(extractDir, os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				erofsSourceDir, err := os.MkdirTemp("", "erofs-source")
				Expect(err).NotTo(HaveOccurred())
				defer os.RemoveAll(erofsSourceDir)

				nmstatectlPath := filepath.Join(erofsSourceDir, "nmstatectl")
				nmstatectlContent := []byte("mock-nmstatectl-binary")
				err = os.WriteFile(nmstatectlPath, nmstatectlContent, 0755) //nolint:gosec
				Expect(err).NotTo(HaveOccurred())

				rootErofsPath := filepath.Join(extractDir, "root.erofs")
				cmd := exec.Command("mkfs.erofs", rootErofsPath, erofsSourceDir)
				err = cmd.Run()
				if err != nil {
					Skip("mkfs.erofs not available, skipping erofs test")
				}

				newNmstateHandler = NewNmstateHandler(workDir, mockExecuter, mockNmstatectlExtractorFactory)

				_, err = newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("nmstatectl extraction from erofs failed"))
			})
		})
	})
})

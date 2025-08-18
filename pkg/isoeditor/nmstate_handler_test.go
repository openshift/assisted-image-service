package isoeditor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang/mock/gomock"

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
			rootfsPath        string
			newNmstateHandler NmstateHandler
			ctrl              *gomock.Controller
			mockExecuter      *MockExecuter
		)

		BeforeEach(func() {
			rootfsPath = filepath.Join(workDir, "rootfs.img")

			// Create a dummy rootfs file
			err := os.WriteFile(rootfsPath, []byte("dummy-rootfs"), 0644) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())

			ctrl = gomock.NewController(GinkgoT())
			mockExecuter = NewMockExecuter(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})
		It("builds nmstate cpio archive", func() {
			// Mock the execution sequence for successful extraction
			gomock.InOrder(
				// First call: extract rootfs with cpio
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				// Second call: list squashfs contents to find nmstatectl
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("/ostree/deploy/rhcos/deploy/abc123/usr/bin/nmstatectl", nil),
				// Third call: extract the nmstatectl binary
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

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			compressedCpio, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(compressedCpio).NotTo(BeEmpty())
		})

		It("handles cpio extraction failure", func() {
			// Mock cpio extraction failure
			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("cpio extraction failed"))

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cpio extraction failed"))
		})

		It("handles squashfs listing failure", func() {
			gomock.InOrder(
				// First call succeeds (cpio extraction)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				// Second call fails (squashfs listing)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("squashfs listing failed")),
			)

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("squashfs listing failed"))
		})

		It("handles nmstatectl binary not found", func() {
			gomock.InOrder(
				// First call succeeds (cpio extraction)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				// Second call succeeds but doesn't return nmstatectl path
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("/some/other/binary", nil),
				// Third call should still be made with empty binary path
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
			)

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no such file or directory"))
		})

		It("handles squashfs extraction failure", func() {
			gomock.InOrder(
				// First call succeeds (cpio extraction)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", nil),
				// Second call succeeds (squashfs listing)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("/ostree/deploy/rhcos/deploy/abc123/usr/bin/nmstatectl", nil),
				// Third call fails (squashfs extraction)
				mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("squashfs extraction failed")),
			)

			newNmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := newNmstateHandler.BuildNmstateCpioArchive(rootfsPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("squashfs extraction failed"))
		})
	})
})

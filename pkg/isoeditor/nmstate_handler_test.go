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

	Describe("CreateNmstateRamDisk", func() {
		var (
			extractDir, ramDiskPath, nmstatectlPath, nmstatectlPathForCaching string
			err                                                               error
			nmstateHandler                                                    NmstateHandler
			ctrl                                                              *gomock.Controller
			mockExecuter                                                      *MockExecuter
			ramDisk                                                           *os.File
		)

		BeforeEach(func() {
			extractDir = os.TempDir()

			nmstateDir := filepath.Join(extractDir, "nmstate", "squashfs-root")
			err = os.MkdirAll(nmstateDir, os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			nmstatectlPath = filepath.Join(nmstateDir, "nmstatectl")
			_, err = os.Create(nmstatectlPath)
			Expect(err).ToNot(HaveOccurred())

			ramDisk, err = os.CreateTemp(extractDir, "nmstate.img")
			Expect(err).ToNot(HaveOccurred())

			nmstatectlPathForCaching = filepath.Join(workDir, "nmstatectl-openshiftVersion-version-arch")

			ramDiskPath = ramDisk.Name()

			ctrl = gomock.NewController(GinkgoT())
			mockExecuter = NewMockExecuter(ctrl)
			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("nmstatectl", nil).Times(3)
			nmstateHandler = NewNmstateHandler(os.TempDir(), mockExecuter)
		})

		AfterEach(func() {
			os.Remove(extractDir)
			os.Remove(ramDiskPath)
		})

		It("ram disk created successfully", func() {
			err = nmstateHandler.CreateNmstateRamDisk("", ramDiskPath, nmstatectlPathForCaching)
			Expect(err).ToNot(HaveOccurred())

			exists, err := fileExists(ramDiskPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("uses cached nmstatectl when available", func() {
			// Pre-create cached nmstatectl file
			cachedContent := []byte("cached-nmstatectl-binary")
			err := os.WriteFile(nmstatectlPathForCaching, cachedContent, 0755) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())

			err = nmstateHandler.CreateNmstateRamDisk("", ramDiskPath, nmstatectlPathForCaching)
			Expect(err).NotTo(HaveOccurred())

			// Verify the ram disk was created with cached content
			ramDiskContent, err := os.ReadFile(ramDiskPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(ramDiskContent).To(Equal(cachedContent))
		})
	})

	Describe("ExtractAndCacheNmstatectl", func() {
		var (
			rootfsPath, nmstatectlCachePath string
			nmstateHandler                  NmstateHandler
			ctrl                            *gomock.Controller
			mockExecuter                    *MockExecuter
		)

		BeforeEach(func() {
			rootfsPath = filepath.Join(workDir, "rootfs.img")
			nmstatectlCachePath = filepath.Join(workDir, "nmstatectl-cache")

			// Create a dummy rootfs file
			err := os.WriteFile(rootfsPath, []byte("dummy-rootfs"), 0644) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())

			ctrl = gomock.NewController(GinkgoT())
			mockExecuter = NewMockExecuter(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("skips extraction when nmstatectl is already cached", func() {
			// Pre-create the cached file
			cachedContent := []byte("already-cached-nmstatectl")
			err := os.WriteFile(nmstatectlCachePath, cachedContent, 0755) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())

			// Should not execute any commands since file is already cached
			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err = nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
			Expect(err).NotTo(HaveOccurred())

			// Verify the original cached content is preserved
			content, err := os.ReadFile(nmstatectlCachePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(cachedContent))
		})

		It("extracts and caches nmstatectl when not already cached", func() {
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

			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			result, err := nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Verify the cache file was created
			_, err = os.Stat(nmstatectlCachePath)
			Expect(err).NotTo(HaveOccurred())
		})

		It("handles cpio extraction failure", func() {
			// Mock cpio extraction failure
			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("cpio extraction failed"))

			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
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

			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
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

			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
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

			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			_, err := nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("squashfs extraction failed"))
		})

		It("handles cached nmstatectl read failure", func() {
			// Create a cached nmstatectl path that exists but will fail to read
			// We create a directory with the same name so os.ReadFile will fail
			err := os.MkdirAll(nmstatectlCachePath, 0755)
			Expect(err).NotTo(HaveOccurred())

			nmstateHandler = NewNmstateHandler(workDir, mockExecuter)

			// The nmstatectl "file" now exists (as a directory) but reading it will fail
			// This tests the error path in ExtractAndCacheNmstatectl line 56
			_, err = nmstateHandler.ExtractAndCacheNmstatectl(rootfsPath, nmstatectlCachePath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read cached nmstatectl"))
		})
	})
})

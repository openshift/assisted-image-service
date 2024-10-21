package isoeditor

import (
	"os"
	"path/filepath"

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
			nmstatectlPath, extractDir, ramDiskPath string
			err                                     error
			nmstateHandler                          NmstateHandler
		)

		BeforeEach(func() {
			extractDir, err = os.MkdirTemp(os.TempDir(), "isoutil")
			Expect(err).ToNot(HaveOccurred())

			nmstatectl, err := os.CreateTemp(extractDir, "nmstatectl")
			Expect(err).ToNot(HaveOccurred())
			nmstatectlPath = nmstatectl.Name()

			ramDisk, err := os.CreateTemp(extractDir, "nmstate.img")
			Expect(err).ToNot(HaveOccurred())
			ramDiskPath = ramDisk.Name()

			nmstateHandler = NewNmstateHandler(os.TempDir())
		})

		AfterEach(func() {
			os.Remove(extractDir)
		})

		It("ram disk created successfully", func() {
			err := nmstateHandler.CreateNmstateRamDisk(nmstatectlPath, ramDiskPath)
			Expect(err).ToNot(HaveOccurred())

			exists, err := fileExists(ramDiskPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("missing nmstatectl binary", func() {
			err := nmstateHandler.CreateNmstateRamDisk("", ramDiskPath)
			Expect(err).To(HaveOccurred())
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("cpu arch mismatch", func() {
			extractDir := os.TempDir()
			err := nmstateHandler.CreateNmstateRamDisk(nmstatectlPath, ramDiskPath)
			Expect(err).ToNot(HaveOccurred())

			exists, err := fileExists(filepath.Join(extractDir, nmstateDiskImagePath))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
	})
})

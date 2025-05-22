package isoeditor

import (
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
		)

		BeforeEach(func() {
			extractDir = os.TempDir()

			nmstateDir := filepath.Join(extractDir, "nmstate", "squashfs-root")
			err = os.MkdirAll(nmstateDir, os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			nmstatectlPath = filepath.Join(nmstateDir, "nmstatectl")
			_, err := os.Create(nmstatectlPath)
			Expect(err).ToNot(HaveOccurred())

			ramDisk, err := os.CreateTemp(extractDir, "nmstate.img")
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
			err := nmstateHandler.CreateNmstateRamDisk("", ramDiskPath, nmstatectlPathForCaching)
			Expect(err).ToNot(HaveOccurred())

			exists, err := fileExists(ramDiskPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
	})
})

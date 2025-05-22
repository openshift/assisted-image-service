package isoeditor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/mock/gomock"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	testRootFSURL     = "https://example.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-live-rootfs.x86_64.img"
	testFCOSRootFSURL = "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/x86_64/fedora-coreos-35.20220103.3.0-live-rootfs.x86_64.img"
)

var _ = Context("with test files", func() {
	var (
		isoFile            string
		filesDir           string
		workDir            string
		minimalISOPath     string
		nmstatectlPath     string
		volumeID           = "Assisted123"
		ctrl               *gomock.Controller
		mockNmstateHandler *MockNmstateHandler
		mockExecuter       *MockExecuter
	)

	validateFileContainsLine := func(filename string, content string) {
		fileContent, err := os.ReadFile(filename)
		Expect(err).NotTo(HaveOccurred())

		found := false
		for _, line := range strings.Split(string(fileContent), "\n") {
			if line == content {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "Failed to find required string `%s` in file `%s`", content, filename)
	}

	BeforeEach(func() {
		filesDir, isoFile = createTestFiles(volumeID)

		var err error
		workDir, err = os.MkdirTemp("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())
		minimalISOPath = filepath.Join(workDir, "minimal.iso")
		nmstatectlPath = filepath.Join(workDir, "nmstatectl-for-caching")
		ctrl = gomock.NewController(GinkgoT())
		mockNmstateHandler = NewMockNmstateHandler(ctrl)
		mockExecuter = NewMockExecuter(ctrl)
		mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("some string", nil).Times(3)
		mockNmstateHandler.EXPECT().CreateNmstateRamDisk(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
		Expect(os.RemoveAll(workDir)).To(Succeed())
	})

	Describe("CreateMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := NewEditor(workDir, mockNmstateHandler)
			err := editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, "x86_64", minimalISOPath, "4.17", nmstatectlPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir, mockNmstateHandler)
			err := editor.CreateMinimalISOTemplate("invalid", testRootFSURL, "x86_64", minimalISOPath, "4.18.0-ec.0", nmstatectlPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateFCOSMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := NewEditor(workDir, mockNmstateHandler)
			err := editor.CreateMinimalISOTemplate(isoFile, testFCOSRootFSURL, "x86_64", minimalISOPath, "4.17", nmstatectlPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir, mockNmstateHandler)
			err := editor.CreateMinimalISOTemplate("invalid", testFCOSRootFSURL, "x86_64", minimalISOPath, "4.18.0-ec.0", nmstatectlPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Fix Config", func() {
		Context("with including nmstate disk image", func() {
			It("fixGrubConfig alters the kernel parameters correctly", func() {
				err := fixGrubConfig(testRootFSURL, filesDir, true)
				Expect(err).ToNot(HaveOccurred())

				newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=%s'"
				grubCfg := fmt.Sprintf(newLine, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

				newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s %s"
				grubCfg = fmt.Sprintf(newLine, ramDiskImagePath, nmstateDiskImagePath)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)
			})

			It("fixIsolinuxConfig alters the kernel parameters correctly", func() {
				err := fixIsolinuxConfig(testRootFSURL, filesDir, true)
				Expect(err).ToNot(HaveOccurred())

				newLine := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
				isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, nmstateDiskImagePath, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "isolinux/isolinux.cfg"), isolinuxCfg)
			})
		})

		Context("without including nmstate disk image", func() {
			It("fixGrubConfig alters the kernel parameters correctly", func() {
				err := fixGrubConfig(testRootFSURL, filesDir, false)
				Expect(err).ToNot(HaveOccurred())

				newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=%s'"
				grubCfg := fmt.Sprintf(newLine, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

				newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
				grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)
			})

			It("fixIsolinuxConfig alters the kernel parameters correctly", func() {
				err := fixIsolinuxConfig(testRootFSURL, filesDir, false)
				Expect(err).ToNot(HaveOccurred())

				newLine := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
				isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "isolinux/isolinux.cfg"), isolinuxCfg)
			})
		})
	})
})

package isoeditor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	testRootFSURL     = "https://example.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-live-rootfs.x86_64.img"
	testFCOSRootFSURL = "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/x86_64/fedora-coreos-35.20220103.3.0-live-rootfs.x86_64.img"
)

var _ = Context("with test files", func() {
	var (
		isoFile        string
		filesDir       string
		workDir        string
		minimalISOPath string
		volumeID       = "Assisted123"
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
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
		Expect(os.RemoveAll(workDir)).To(Succeed())
	})

	Describe("CreateMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			nmstatectl, err := os.CreateTemp(os.TempDir(), "nmstatectl")
			Expect(err).ToNot(HaveOccurred())

			editor := NewEditor(workDir, nmstatectl.Name())
			err = editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, "x86_64", minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir, "")
			err := editor.CreateMinimalISOTemplate("invalid", testRootFSURL, "x86_64", minimalISOPath)
			Expect(err).To(HaveOccurred())
		})

		It("missing nmstatectl binary", func() {
			editor := NewEditor(workDir, "")
			err := editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, "x86_64", minimalISOPath)
			Expect(err).To(HaveOccurred())
		})

		It("cpu arch mismatch", func() {
			nmstatectl, err := os.CreateTemp(os.TempDir(), "nmstatectl")
			Expect(err).ToNot(HaveOccurred())

			editor := NewEditor(workDir, nmstatectl.Name())
			err = editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, "some-arch", minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("CreateFCOSMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			nmstatectl, err := os.CreateTemp(os.TempDir(), "nmstatectl")
			Expect(err).ToNot(HaveOccurred())

			editor := NewEditor(workDir, nmstatectl.Name())

			err = editor.CreateMinimalISOTemplate(isoFile, testFCOSRootFSURL, "x86_64", minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir, "")
			err := editor.CreateMinimalISOTemplate("invalid", testFCOSRootFSURL, "x86_64", minimalISOPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("createNmstateRamDisk", func() {
		var (
			nmstatectlPath, extractDir, arch, ramDiskPath string
			err                                           error
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

			arch = normalizeCPUArchitecture(runtime.GOARCH)
		})

		AfterEach(func() {
			os.Remove(extractDir)
		})

		It("ram disk created successfully", func() {
			err := createNmstateRamDisk(arch, nmstatectlPath, ramDiskPath)
			Expect(err).ToNot(HaveOccurred())

			exists, err := fileExists(ramDiskPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("missing nmstatectl binary", func() {
			err := createNmstateRamDisk(arch, "", ramDiskPath)
			Expect(err).To(HaveOccurred())
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("cpu arch mismatch", func() {
			extractDir := os.TempDir()
			err := createNmstateRamDisk("some-arch", nmstatectlPath, ramDiskPath)
			Expect(err).ToNot(HaveOccurred())

			exists, err := fileExists(filepath.Join(extractDir, nmstateDiskImagePath))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
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

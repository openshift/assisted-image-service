package isoeditor

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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
		fileContent, err := ioutil.ReadFile(filename)
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
		workDir, err = ioutil.TempDir("", "testisoeditor")
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
			editor := NewEditor(workDir)

			err := editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir)
			err := editor.CreateMinimalISOTemplate("invalid", testRootFSURL, minimalISOPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateFCOSMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := NewEditor(workDir)

			err := editor.CreateMinimalISOTemplate(isoFile, testFCOSRootFSURL, minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir)
			err := editor.CreateMinimalISOTemplate("invalid", testFCOSRootFSURL, minimalISOPath)
			Expect(err).To(HaveOccurred())
		})
	})
	It("fixGrubConfig alters the kernel parameters correctly", func() {
		err := fixGrubConfig(testRootFSURL, filesDir)
		Expect(err).ToNot(HaveOccurred())

		newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=%s'"
		grubCfg := fmt.Sprintf(newLine, testRootFSURL)
		validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

		newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
		grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
		validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

	})
	It("fixIsolinuxConfig alters the kernel parameters correctly", func() {
		err := fixIsolinuxConfig(testRootFSURL, filesDir)
		Expect(err).ToNot(HaveOccurred())

		newLine := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
		isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, testRootFSURL)
		validateFileContainsLine(filepath.Join(filesDir, "isolinux/isolinux.cfg"), isolinuxCfg)
	})
})

package isoeditor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	testRootFSURL         = "https://example.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-live-rootfs.x86_64.img"
	ignitionPaddingLength = 256 * 1024 // 256KB
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
			err := embedOffsetsInSystemArea(isoFile)
			Expect(err).ToNot(HaveOccurred())

			err = editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
		})

		It("missing iso file", func() {
			editor := NewEditor(workDir)
			err := editor.CreateMinimalISOTemplate("invalid", testRootFSURL, minimalISOPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("fixTemplateConfigs", func() {
		It("alters the kernel parameters correctly", func() {
			err := fixTemplateConfigs(testRootFSURL, filesDir)
			Expect(err).ToNot(HaveOccurred())

			newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=%s'"
			grubCfg := fmt.Sprintf(newLine, testRootFSURL)
			validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

			newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
			grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
			validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

			newLine = "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
			isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, testRootFSURL)
			validateFileContainsLine(filepath.Join(filesDir, "isolinux/isolinux.cfg"), isolinuxCfg)
		})
	})

	Describe("embedOffsetsInSystemArea", func() {
		getOffsetInfo := func(f *os.File, loc int64) *OffsetInfo {
			meta := make([]byte, 24)
			count, err := f.ReadAt(meta, loc)
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(24))

			buf := bytes.NewReader(meta)
			info := new(OffsetInfo)
			err = binary.Read(buf, binary.LittleEndian, info)
			Expect(err).ToNot(HaveOccurred())

			return info
		}

		It("embeds offsets in system area correctly", func() {
			editor := NewEditor(workDir)

			// Create template
			err := editor.CreateMinimalISOTemplate(isoFile, testRootFSURL, minimalISOPath)
			Expect(err).ToNot(HaveOccurred())

			// Read offsets
			f, err := os.OpenFile(minimalISOPath, os.O_RDONLY, 0o664)
			Expect(err).ToNot(HaveOccurred())
			ignitionOffsetInfo := getOffsetInfo(f, 32744)
			ramDiskOffsetInfo := getOffsetInfo(f, 32720)

			// Validate ignitionOffsetInfo
			ignitionOffset, err := GetFileLocation(ignitionImagePath, minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ignitionOffsetInfo.Key[:])).To(Equal(ignitionHeaderKey))
			Expect(ignitionOffsetInfo.Offset).To(Equal(ignitionOffset))
			Expect(ignitionOffsetInfo.Length).To(Equal(uint64(ignitionPaddingLength)))

			// Validate ramDiskOffsetInfo
			ramDiskOffset, err := GetFileLocation(ramDiskImagePath, minimalISOPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ramDiskOffsetInfo.Key[:])).To(Equal(ramdiskHeaderKey))
			Expect(ramDiskOffsetInfo.Offset).To(Equal(ramDiskOffset))
			Expect(ramDiskOffsetInfo.Length).To(Equal(RamDiskPaddingLength))
		})
	})
})

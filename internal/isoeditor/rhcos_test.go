package isoeditor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carbonin/assisted-image-service/internal/isoutil"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

const (
	testRootFSURL = "https://example.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-live-rootfs.x86_64.img"
)

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func TestIsoEditor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IsoEditor")
}

var _ = Context("with test files", func() {
	var (
		isoDir   string
		isoFile  string
		filesDir string
		workDir  string
		volumeID = "Assisted123"
	)

	BeforeSuite(func() {
		filesDir, isoDir, isoFile = createIsoViaGenisoimage(volumeID)
	})

	AfterSuite(func() {
		os.RemoveAll(filesDir)
		os.RemoveAll(isoDir)
	})

	BeforeEach(func() {
		var err error
		workDir, err = ioutil.TempDir("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		err := os.RemoveAll(workDir)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("CreateMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := editorForFile(isoFile, workDir)
			err := editor.(*rhcosEditor).embedOffsetsInSystemArea(isoFile)
			Expect(err).ToNot(HaveOccurred())
			file, err := editor.CreateMinimalISOTemplate(testRootFSURL)
			Expect(err).ToNot(HaveOccurred())

			// Creating the template should remove the working directory
			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})

		It("missing iso file", func() {
			editor := editorForFile("invalid", workDir)
			_, err := editor.CreateMinimalISOTemplate(testRootFSURL)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("fixTemplateConfigs", func() {
		It("alters the kernel parameters correctly", func() {
			editor := editorForFile(isoFile, workDir)
			isoHandler := editor.(*rhcosEditor).isoHandler

			err := isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			err = editor.(*rhcosEditor).fixTemplateConfigs(testRootFSURL)
			Expect(err).ToNot(HaveOccurred())

			newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=%s'"
			grubCfg := fmt.Sprintf(newLine, testRootFSURL)
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
			grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			newLine = "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
			isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, testRootFSURL)
			validateFileContainsLine(isoHandler.ExtractedPath("isolinux/isolinux.cfg"), isolinuxCfg)
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
			editor := editorForFile(isoFile, workDir)

			// Create template
			isoPath, err := editor.CreateMinimalISOTemplate(testRootFSURL)
			Expect(err).ToNot(HaveOccurred())

			// Read offsets
			f, err := os.OpenFile(isoPath, os.O_RDONLY, 0o664)
			Expect(err).ToNot(HaveOccurred())
			ignitionOffsetInfo := getOffsetInfo(f, 32744)
			ramDiskOffsetInfo := getOffsetInfo(f, 32720)

			// Validate ignitionOffsetInfo
			ignitionOffset, err := isoutil.GetFileLocation(ignitionImagePath, isoPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ignitionOffsetInfo.Key[:])).To(Equal(ignitionHeaderKey))
			Expect(ignitionOffsetInfo.Offset).To(Equal(ignitionOffset))
			Expect(ignitionOffsetInfo.Length).To(Equal(IgnitionPaddingLength))

			// Validate ramDiskOffsetInfo
			ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ramDiskOffsetInfo.Key[:])).To(Equal(ramdiskHeaderKey))
			Expect(ramDiskOffsetInfo.Offset).To(Equal(ramDiskOffset))
			Expect(ramDiskOffsetInfo.Length).To(Equal(RamDiskPaddingLength))
		})
	})
})

func editorForFile(iso string, workDir string) Editor {
	return &rhcosEditor{
		isoHandler: isoutil.NewHandler(iso, workDir),
		log:        getTestLog(),
	}
}

func createIsoViaGenisoimage(volumeID string) (string, string, string) {
	grubConfig := `
menuentry 'RHEL CoreOS (Live)' --class fedora --class gnu-linux --class gnu --class os {
	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
	initrd /images/pxeboot/initrd.img /images/ignition.img
}
`
	isoLinuxConfig := `
label linux
  menu label ^RHEL CoreOS (Live)
  menu default
  kernel /images/pxeboot/vmlinuz
  append initrd=/images/pxeboot/initrd.img,/images/ignition.img random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
`

	filesDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())

	isoDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())
	isoFile := filepath.Join(isoDir, "test.iso")

	err = os.Mkdir(filepath.Join(filesDir, "files"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/images/pxeboot"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/EFI/redhat"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/EFI/redhat/grub.cfg"), []byte(grubConfig), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/isolinux"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/isolinux/isolinux.cfg"), []byte(isoLinuxConfig), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/assisted_installer_custom.img"), make([]byte, RamDiskPaddingLength), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/ignition.img"), make([]byte, IgnitionPaddingLength), 0600)
	Expect(err).ToNot(HaveOccurred())
	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", volumeID, "-o", isoFile, filepath.Join(filesDir, "files"))
	err = cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	return filesDir, isoDir, isoFile
}

func validateFileContainsLine(filename string, content string) {
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

package isoeditor

import (
	"encoding/json"
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
				// Pass nil for kargs since we're just testing file changes
				err := fixGrubConfig(testRootFSURL, filesDir, true, nil)
				Expect(err).ToNot(HaveOccurred())

				newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=\"%s\""
				grubCfg := fmt.Sprintf(newLine, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

				newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s %s"
				grubCfg = fmt.Sprintf(newLine, ramDiskImagePath, nmstateDiskImagePath)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)
			})

			It("fixIsolinuxConfig alters the kernel parameters correctly", func() {
				// Pass nil for kargs since we're just testing file changes
				err := fixIsolinuxConfig(testRootFSURL, filesDir, true, nil)
				Expect(err).ToNot(HaveOccurred())

				newLine := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=\"%s\""
				isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, nmstateDiskImagePath, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "isolinux/isolinux.cfg"), isolinuxCfg)
			})
		})

		Context("without including nmstate disk image", func() {
			It("fixGrubConfig alters the kernel parameters correctly", func() {
				// Pass nil for kargs since we're just testing file changes
				err := fixGrubConfig(testRootFSURL, filesDir, false, nil)
				Expect(err).ToNot(HaveOccurred())

				newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=\"%s\""
				grubCfg := fmt.Sprintf(newLine, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)

				newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
				grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
				validateFileContainsLine(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), grubCfg)
			})

			It("fixIsolinuxConfig alters the kernel parameters correctly", func() {
				// Pass nil for kargs since we're just testing file changes
				err := fixIsolinuxConfig(testRootFSURL, filesDir, false, nil)
				Expect(err).ToNot(HaveOccurred())

				newLine := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=\"%s\""
				isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, testRootFSURL)
				validateFileContainsLine(filepath.Join(filesDir, "isolinux/isolinux.cfg"), isolinuxCfg)
			})
		})

		Context("URL validation", func() {
			It("rejects URLs containing $ character", func() {
				invalidURL := "https://example.com/test$invalid/rootfs.img"
				err := fixGrubConfig(invalidURL, filesDir, false, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid rootfs URL: contains invalid character '$'"))

				err = fixIsolinuxConfig(invalidURL, filesDir, false, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid rootfs URL: contains invalid character '$'"))
			})

			It("rejects URLs containing \\ character", func() {
				invalidURL := "https://example.com/test\\invalid/rootfs.img"
				err := fixGrubConfig(invalidURL, filesDir, false, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid rootfs URL: contains invalid character '\\'"))

				err = fixIsolinuxConfig(invalidURL, filesDir, false, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid rootfs URL: contains invalid character '\\'"))
			})

			It("accepts valid URLs", func() {
				validURL := "https://example.com/valid/rootfs.img"
				err := fixGrubConfig(validURL, filesDir, false, nil)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("editString function", func() {
			It("replaces content in named capture group", func() {
				content := "line1\nline2\nline3"
				newContent, err := editString(content, `(?P<replace>line2)`, "modified")
				Expect(err).ToNot(HaveOccurred())
				Expect(newContent).To(Equal("line1\nmodified\nline3"))
			})

			It("appends content when replace group is at end", func() {
				content := "some text"
				newContent, err := editString(content, `(some text)(?P<replace>$)`, " more")
				Expect(err).ToNot(HaveOccurred())
				Expect(newContent).To(Equal("some text more"))
			})

			It("returns error if no replace group", func() {
				content := "some text"
				_, err := editString(content, `some text`, "replacement")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("must have a named capture group called 'replace'"))
			})

			It("returns original content if no match", func() {
				content := "some text"
				newContent, err := editString(content, `(?P<replace>nomatch)`, "replacement")
				Expect(err).ToNot(HaveOccurred())
				Expect(newContent).To(Equal(content))
			})
		})

		Context("kargs.json handling", func() {
			It("successfully round-trips kargs.json", func() {
				// Create a temporary directory with a kargs.json file
				tmpDir, err := os.MkdirTemp("", "kargs-test")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(tmpDir)

				kargsDir := filepath.Join(tmpDir, "coreos")
				err = os.MkdirAll(kargsDir, 0755)
				Expect(err).ToNot(HaveOccurred())

				// Create EFI/fedora and isolinux directories
				err = os.MkdirAll(filepath.Join(tmpDir, "EFI/fedora"), 0755)
				Expect(err).ToNot(HaveOccurred())
				err = os.MkdirAll(filepath.Join(tmpDir, "isolinux"), 0755)
				Expect(err).ToNot(HaveOccurred())

				// Create grub.cfg
				grubConfig := "\tlinux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.liveiso=rhcos-416.94.202404301731-0\n\tinitrd /images/pxeboot/initrd.img /images/ignition.img\n"
				err = os.WriteFile(filepath.Join(tmpDir, "EFI/fedora/grub.cfg"), []byte(grubConfig), 0600)
				Expect(err).ToNot(HaveOccurred())

				// Create isolinux.cfg
				isolinuxConfig := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.liveiso=rhcos-416.94.202404301731-0\n"
				err = os.WriteFile(filepath.Join(tmpDir, "isolinux/isolinux.cfg"), []byte(isolinuxConfig), 0600)
				Expect(err).ToNot(HaveOccurred())

				kargsPath := filepath.Join(kargsDir, "kargs.json")
				originalJSON := `{
  "default": "random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.liveiso=rhcos-416.94.202404301731-0",
  "files": [
    {
      "path": "EFI/fedora/grub.cfg",
      "offset": 1000,
      "size": 2000
    }
  ],
  "size": 1024
}`
				err = os.WriteFile(kargsPath, []byte(originalJSON), 0600)
				Expect(err).ToNot(HaveOccurred())

				// Call updateKargs with a rootFS URL (no nmstate, x86_64 arch)
				err = updateKargs(tmpDir, testRootFSURL, false, "x86_64")
				Expect(err).ToNot(HaveOccurred())

				// Read the file back and verify it's valid JSON
				updatedData, err := os.ReadFile(kargsPath)
				Expect(err).ToNot(HaveOccurred())

				var config kargsConfig
				err = json.Unmarshal(updatedData, &config)
				Expect(err).ToNot(HaveOccurred())

				// Verify the default kargs were updated:
				// - coreos.liveiso parameter should be removed
				// - coreos.live.rootfs_url should be added
				Expect(config.Default).To(ContainSubstring("coreos.live.rootfs_url"))
				Expect(config.Default).To(ContainSubstring(testRootFSURL))
				Expect(config.Default).ToNot(ContainSubstring("coreos.liveiso"))

				// Verify size was updated to reflect the change in default kargs length
				// Removed: "coreos.liveiso=rhcos-416.94.202404301731-0 " (43 chars)
				// Added: " coreos.live.rootfs_url=\"https://example.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-live-rootfs.x86_64.img\"" (121 chars)
				// Net change: 121 - 43 = 78 chars
				Expect(config.Size).To(Equal(int64(1024 + 78)))
			})
		})
	})
})

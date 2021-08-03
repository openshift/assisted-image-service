package isoeditor

import (
	"io/ioutil"
	"os"
	"path/filepath"

	diskfs "github.com/diskfs/go-diskfs"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Context("with test files", func() {
	var (
		isoFile  string
		filesDir string
		volumeID = "Assisted123"
	)

	validateFileContent := func(filename string, content string) {
		fileContent, err := ioutil.ReadFile(filename)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(fileContent)).To(Equal(content))
	}

	BeforeEach(func() {
		filesDir, isoFile = createTestFiles(volumeID)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
	})

	Describe("Extract", func() {
		It("extracts the files from an iso", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)

			Expect(Extract(isoFile, dir)).To(Succeed())

			validateFileContent(filepath.Join(dir, "images/pxeboot/rootfs.img"), "this is rootfs")
			validateFileContent(filepath.Join(dir, "EFI/redhat/grub.cfg"), testGrubConfig)
			validateFileContent(filepath.Join(dir, "isolinux/isolinux.cfg"), testISOLinuxConfig)
			validateFileContent(filepath.Join(dir, "isolinux/boot.cat"), "")
		})
	})

	Describe("Create", func() {
		It("generates an iso with the content in the given directory", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			isoPath := filepath.Join(dir, "test.iso")

			Expect(Create(isoPath, filesDir, "my-vol")).To(Succeed())

			d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
			Expect(err).ToNot(HaveOccurred())
			fs, err := d.GetFilesystem(0)
			Expect(err).ToNot(HaveOccurred())

			f, err := fs.OpenFile("/images/pxeboot/rootfs.img", os.O_RDONLY)
			Expect(err).ToNot(HaveOccurred())
			content, err := ioutil.ReadAll(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("this is rootfs"))

			f, err = fs.OpenFile("/isolinux/boot.cat", os.O_RDONLY)
			Expect(err).ToNot(HaveOccurred())
			content, err = ioutil.ReadAll(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal(""))
		})
	})

	Describe("fileExists", func() {
		It("returns true when file exists", func() {
			exists, err := fileExists(filepath.Join(filesDir, "images/ignition.img"))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())

			exists, err = fileExists(filepath.Join(filesDir, "images/efiboot.img"))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("returns false when file does not exist", func() {
			exists, err := fileExists(filepath.Join(filesDir, "asdf"))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())

			exists, err = fileExists(filepath.Join(filesDir, "missingdir/things"))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
	})

	Describe("haveBootFiles", func() {
		It("returns true when boot files are present", func() {
			bootFilesDir, err := os.MkdirTemp("", "bootfiles")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(bootFilesDir)

			Expect(os.Mkdir(filepath.Join(bootFilesDir, "isolinux"), 0755)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(bootFilesDir, "images"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(bootFilesDir, "isolinux/boot.cat"), []byte("boot.cat"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(bootFilesDir, "isolinux/isolinux.bin"), []byte("isolinux.bin"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(bootFilesDir, "images/efiboot.img"), []byte("efiboot.img"), 0600)).To(Succeed())

			haveBootFiles, err := haveBootFiles(bootFilesDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeTrue())
		})

		It("returns false when boot files are not present", func() {
			p, err := filepath.Abs(filepath.Join(filesDir, "images"))
			Expect(err).ToNot(HaveOccurred())

			haveBootFiles, err := haveBootFiles(p)
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeFalse())
		})
	})

	Describe("VolumeIdentifier", func() {
		It("returns the correct value", func() {
			id, err := VolumeIdentifier(isoFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(volumeID))
		})
	})

	Describe("efiLoadSectors", func() {
		It("returns the correct value", func() {
			sectors, err := efiLoadSectors(filesDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(sectors).To(Equal(uint16(3997)))
		})
	})
})

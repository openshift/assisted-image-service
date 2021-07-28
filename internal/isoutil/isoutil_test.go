package isoutil

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIsoUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Iso Util")
}

var _ = Context("with test files", func() {
	var (
		isoDir   string
		isoFile  string
		filesDir string
		volumeID = "Assisted123"
	)
	BeforeEach(func() {
		filesDir, isoDir, isoFile = createIsoViaGenisoimage(volumeID)
	})

	AfterEach(func() {
		os.RemoveAll(filesDir)
		os.RemoveAll(isoDir)
	})

	Describe("Extract", func() {
		It("extracts the files from an iso", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)

			Expect(Extract(isoFile, dir)).To(Succeed())

			validateFileContent(filepath.Join(dir, "test"), "testcontent\n")
			validateFileContent(filepath.Join(dir, "testdir/stuff"), "morecontent\n")
		})
	})

	Describe("Create", func() {
		It("generates an iso with the content in the given directory", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			isoPath := filepath.Join(dir, "test.iso")

			Expect(Create(isoPath, filepath.Join(filesDir, "files"), "my-vol")).To(Succeed())

			d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
			Expect(err).ToNot(HaveOccurred())
			fs, err := d.GetFilesystem(0)
			Expect(err).ToNot(HaveOccurred())

			f, err := fs.OpenFile("/test", os.O_RDONLY)
			Expect(err).ToNot(HaveOccurred())
			content, err := ioutil.ReadAll(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("testcontent\n"))

			f, err = fs.OpenFile("/testdir/stuff", os.O_RDONLY)
			Expect(err).ToNot(HaveOccurred())
			content, err = ioutil.ReadAll(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("morecontent\n"))
		})
	})

	Describe("fileExists", func() {
		It("returns true when file exists", func() {
			exists, err := fileExists(filepath.Join(filesDir, "files/test"))
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())

			exists, err = fileExists(filepath.Join(filesDir, "files/testdir/stuff"))
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
			p, err := filepath.Abs(filepath.Join(filesDir, "boot_files"))
			Expect(err).ToNot(HaveOccurred())

			haveBootFiles, err := haveBootFiles(p)
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeTrue())
		})

		It("returns false when boot files are not present", func() {
			p, err := filepath.Abs(filepath.Join(filesDir, "files"))
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
			p, err := filepath.Abs(filepath.Join(filesDir, "boot_files"))
			Expect(err).ToNot(HaveOccurred())

			sectors, err := efiLoadSectors(p)
			Expect(err).ToNot(HaveOccurred())
			Expect(sectors).To(Equal(uint16(3997)))
		})
	})
})

func createIsoViaGenisoimage(volumeID string) (string, string, string) {
	filesDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())

	isoDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())
	isoFile := filepath.Join(isoDir, "test.iso")

	err = os.Mkdir(filepath.Join(filesDir, "files"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files", "test"), []byte("testcontent\n"), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "files", "testdir"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files", "testdir", "stuff"), []byte("morecontent\n"), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "boot_files"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "boot_files", "images"), 0755)
	Expect(err).ToNot(HaveOccurred())
	// Create a file with some size to test load sector calculation
	f, err := os.Create(filepath.Join(filesDir, "boot_files", "images", "efiboot.img"))
	Expect(err).ToNot(HaveOccurred())
	err = f.Truncate(8184422)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "boot_files", "isolinux"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "boot_files", "isolinux", "boot.cat"), []byte(""), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "boot_files", "isolinux", "isolinux.bin"), []byte(""), 0600)
	Expect(err).ToNot(HaveOccurred())
	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", volumeID, "-o", isoFile, filepath.Join(filesDir, "files"))
	err = cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	return filesDir, isoDir, isoFile
}

func validateFileContent(filename string, content string) {
	fileContent, err := ioutil.ReadFile(filename)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(fileContent)).To(Equal(content))
}

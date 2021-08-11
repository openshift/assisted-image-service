package isoeditor

import (
	"bytes"
	"io"
	"os"

	diskfs "github.com/diskfs/go-diskfs"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewRHCOSStreamReader", func() {
	var (
		isoFile  string
		filesDir string
	)

	BeforeEach(func() {
		filesDir, isoFile = createTestFiles("Assisted123")
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
	})

	isoFileContent := func(isoPath, filePath string) []byte {
		d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
		Expect(err).NotTo(HaveOccurred())

		fs, err := d.GetFilesystem(0)
		Expect(err).NotTo(HaveOccurred())

		fsFile, err := fs.OpenFile(filePath, os.O_RDONLY)
		Expect(err).NotTo(HaveOccurred())
		contentBytes, err := io.ReadAll(fsFile)
		Expect(err).NotTo(HaveOccurred())

		// Embedded files will always have trailing nulls
		return bytes.TrimRight(contentBytes, "\x00")
	}

	It("embeds the ignition", func() {
		ignitionContent := []byte("someignitioncontent")
		streamReader, err := NewRHCOSStreamReader(isoFile, ignitionContent)
		Expect(err).NotTo(HaveOccurred())

		f, err := os.CreateTemp(filesDir, "streamed*.iso")
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(f, streamReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Sync()).To(Succeed())
		Expect(f.Close()).To(Succeed())

		Expect(isoFileContent(f.Name(), ignitionImagePath)).To(Equal(ignitionContent))
	})
})

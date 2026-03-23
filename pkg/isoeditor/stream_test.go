package isoeditor

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	diskfs "github.com/diskfs/go-diskfs"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewRHCOSStreamReader", func() {
	var (
		isoFile         string
		filesDir        string
		ignitionContent = []byte("someignitioncontent")
		// Note: trailing 0 bytes are omitted because we are comparing to file
		// content that has trailing 0 bytes stripped.
		ignitionArchiveBytes = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 50, 48, 55, 48, 55, 48,
			52, 128, 0, 48, 109, 97, 232, 104, 98, 128, 29, 24, 162, 113, 141, 113,
			168, 67, 7, 78, 48, 70, 114, 126, 94, 90, 102, 186, 94, 102, 122, 30,
			3, 3, 3, 67, 113, 126, 110, 106, 102, 122, 94, 102, 73, 102, 126, 94,
			114, 126, 94, 73, 106, 94, 9, 3, 138, 123, 8, 1, 98, 213, 225, 116,
			79, 72, 144, 163, 167, 143, 107, 144, 162, 162, 34, 200, 61, 128, 0, 0,
			0, 255, 255, 191, 236, 44, 242, 12, 1}
	)

	BeforeEach(func() {
		filesDir, isoFile = createTestFiles("Assisted123")
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
	})

	isoFileContent := func(isoPath, filePath string) []byte {
		d, err := diskfs.Open(isoPath, diskfs.WithOpenMode(diskfs.ReadOnly))
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

	It("embeds the ignition with no ramdisk content", func() {
		streamReader, err := NewRHCOSStreamReader(isoFile, &IgnitionContent{Config: ignitionContent}, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		f, err := os.CreateTemp(filesDir, "streamed*.iso")
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(f, streamReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Sync()).To(Succeed())
		Expect(f.Close()).To(Succeed())

		Expect(isoFileContent(f.Name(), ignitionImagePath)).To(Equal(ignitionArchiveBytes))
	})

	It("embeds the ignition and ramdisk content", func() {
		initrdContent := []byte("someramdiskcontent")
		streamReader, err := NewRHCOSStreamReader(isoFile, &IgnitionContent{Config: ignitionContent}, initrdContent, nil)
		Expect(err).NotTo(HaveOccurred())

		f, err := os.CreateTemp(filesDir, "streamed*.iso")
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(f, streamReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Sync()).To(Succeed())
		Expect(f.Close()).To(Succeed())

		Expect(isoFileContent(f.Name(), ignitionImagePath)).To(Equal(ignitionArchiveBytes))
		Expect(isoFileContent(f.Name(), ramDiskImagePath)).To(Equal(initrdContent))
	})
	It("embeds the ignition and kargs content", func() {
		kargs := []byte(" p1 p2 p3 p4\n")
		streamReader, err := NewRHCOSStreamReader(isoFile, &IgnitionContent{Config: ignitionContent}, nil, kargs)
		Expect(err).NotTo(HaveOccurred())

		f, err := os.CreateTemp(filesDir, "streamed*.iso")
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(f, streamReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Sync()).To(Succeed())
		Expect(f.Close()).To(Succeed())

		Expect(isoFileContent(f.Name(), ignitionImagePath)).To(Equal(ignitionArchiveBytes))
		grubFileContent := string(isoFileContent(f.Name(), defaultGrubFilePath))
		isolinuxContent := string(isoFileContent(f.Name(), defaultIsolinuxFilePath))
		for _, content := range []string{grubFileContent, isolinuxContent} {
			Expect(content).To(MatchRegexp(string(kargs) + "#+ COREOS_KARG_EMBED_AREA"))
		}
	})

	It("Embeds the ignition in a ISO that uses the 'igninfo.json' file", func() {
		// Create input ISO:
		tmpDir, inputFile := createS390TestFiles("Assisted123", 0)
		defer func() {
			Expect(os.RemoveAll(tmpDir)).To(Succeed())
			Expect(os.Remove(inputFile)).To(Succeed())
		}()

		// Copy the output ISO to a file:
		outputReader, err := NewRHCOSStreamReader(inputFile, &IgnitionContent{Config: ignitionContent}, nil, nil)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			Expect(outputReader.Close()).To(Succeed())
		}()
		outputFd, err := os.CreateTemp(tmpDir, "streamed*.iso")
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			Expect(outputFd.Close()).To(Succeed())
			Expect(os.Remove(outputFd.Name())).To(Succeed())
		}()
		_, err = io.Copy(outputFd, outputReader)
		Expect(err).ToNot(HaveOccurred())
		Expect(outputFd.Sync()).To(Succeed())

		// Read the 'igninfo.json' file:
		ignInfoBytes, err := ReadFileFromISO(inputFile, ignitionInfoPath)
		Expect(err).ToNot(HaveOccurred())
		var ignInfo ignitionInfo
		Expect(json.Unmarshal(ignInfoBytes, &ignInfo)).To(Succeed())

		// Read the ignition content. Note that this will read the complete content of the
		// area containing the ignition content, including the trailing zeros. We need to
		// remove those before comparing to the input ignition.
		containerBytes := isoFileContent(outputFd.Name(), ignInfo.File)
		ignitionBytes := containerBytes[ignInfo.Offset : ignInfo.Offset+ignInfo.Length]
		ignitionBytes = bytes.TrimRight(ignitionBytes, "\x00")

		// Compare the actual ignition from the ISO with the input:
		Expect(ignitionBytes).To(Equal(ignitionArchiveBytes))
	})
})

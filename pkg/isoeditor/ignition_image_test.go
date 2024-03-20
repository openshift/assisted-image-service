package isoeditor

import (
	"io"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("IgnitionContent.Archive", func() {
	var (
		isoFile              string
		filesDir             string
		ignitionContent      = []byte("someignitioncontent")
		ignitionArchiveBytes = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 50, 48, 55, 48, 55, 48,
			52, 128, 0, 48, 109, 97, 232, 104, 98, 128, 29, 24, 162, 113, 141, 113,
			168, 67, 7, 78, 48, 70, 114, 126, 94, 90, 102, 186, 94, 102, 122, 30,
			3, 3, 3, 67, 113, 126, 110, 106, 102, 122, 94, 102, 73, 102, 126, 94,
			114, 126, 94, 73, 106, 94, 9, 3, 138, 123, 8, 1, 98, 213, 225, 116,
			79, 72, 144, 163, 167, 143, 107, 144, 162, 162, 34, 200, 61, 128, 0, 0,
			0, 255, 255, 191, 236, 44, 242, 12, 1, 0, 0}
	)

	BeforeEach(func() {
		filesDir, isoFile = createTestFiles("Assisted123")
	})

	AfterEach(func() {
		Expect(os.RemoveAll(filesDir)).To(Succeed())
		Expect(os.Remove(isoFile)).To(Succeed())
	})

	It("streams the ignition image", func() {
		content := IgnitionContent{ignitionContent}

		outputs, err := NewIgnitionImageReader(isoFile, &content)
		Expect(err).NotTo(HaveOccurred())

		Expect(len(outputs)).To(Equal(1))
		imgBytes, err := io.ReadAll(outputs[0].Data)
		Expect(err).NotTo(HaveOccurred())
		Expect(imgBytes[:len(ignitionArchiveBytes)]).To(Equal(ignitionArchiveBytes))
		Expect(len(imgBytes)).To(Equal(256 * 1024))
		for i := len(ignitionArchiveBytes); i < len(imgBytes); i++ {
			Expect(imgBytes[i]).To(Equal(byte(0)))
		}
		Expect(outputs[0].Filename).To(Equal("images/ignition.img"))
	})
})

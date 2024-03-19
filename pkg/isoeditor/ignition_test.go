package isoeditor

import (
	"io"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("IgnitionContent.Archive", func() {
	var (
		ignitionContent      = []byte("someignitioncontent")
		ignitionArchiveBytes = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 50, 48, 55, 48, 55, 48,
			52, 128, 0, 48, 109, 97, 232, 104, 98, 128, 29, 24, 162, 113, 141, 113,
			168, 67, 7, 78, 48, 70, 114, 126, 94, 90, 102, 186, 94, 102, 122, 30,
			3, 3, 3, 67, 113, 126, 110, 106, 102, 122, 94, 102, 73, 102, 126, 94,
			114, 126, 94, 73, 106, 94, 9, 3, 138, 123, 8, 1, 98, 213, 225, 116,
			79, 72, 144, 163, 167, 143, 107, 144, 162, 162, 34, 200, 61, 128, 0, 0,
			0, 255, 255, 191, 236, 44, 242, 12, 1, 0, 0, 0}
	)

	It("converts the ignition to a compressed CPIO archive", func() {
		content := IgnitionContent{ignitionContent}

		data, err := content.Archive()
		Expect(err).NotTo(HaveOccurred())

		ignitionBytes, err := io.ReadAll(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(ignitionBytes).To(Equal(ignitionArchiveBytes))
		Expect(len(ignitionBytes) % 4).To(Equal(0))
	})
})

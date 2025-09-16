package isoeditor

import (
	"bytes"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewInitrdAddrsizeReader", func() {
	var (
		ignitionContent = []byte("someignitioncontent")
		initrdAddrsize  = []byte{
			1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 4, 0, 108}
	)

	filesDir, _ := createS390TestFiles("Assisted123", 2560000)
	initrdPath := filepath.Join(filesDir, "images/ignition.img")
	addrsizePath := filepath.Join(filesDir, "images/initrd.addrsize")
	It("Get initrd.addrsize file", func() {
		streamReader, err := NewInitRamFSStreamReader(initrdPath, &IgnitionContent{Config: ignitionContent})
		Expect(err).NotTo(HaveOccurred())

		addrsizeFile, err := NewInitrdAddrsizeReader(addrsizePath, streamReader)
		Expect(err).NotTo(HaveOccurred())
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(addrsizeFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(initrdAddrsize).To(Equal(buf.Bytes()))

	})
})

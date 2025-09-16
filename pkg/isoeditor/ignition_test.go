package isoeditor

import (
	"bytes"
	"compress/gzip"
	"io"

	"github.com/cavaliercoder/go-cpio"
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

	extractCPIOFiles := func(archiveBytes []byte) map[string][]byte {
		gzReader, err := gzip.NewReader(bytes.NewReader(archiveBytes))
		Expect(err).NotTo(HaveOccurred())
		defer gzReader.Close()

		cpioReader := cpio.NewReader(gzReader)
		files := make(map[string][]byte)

		for {
			header, err := cpioReader.Next()
			if err == io.EOF {
				break
			}
			Expect(err).NotTo(HaveOccurred())

			content, err := io.ReadAll(cpioReader)
			Expect(err).NotTo(HaveOccurred())
			files[header.Name] = content
		}
		return files
	}

	It("converts the ignition to a compressed CPIO archive", func() {
		content := IgnitionContent{Config: ignitionContent}

		data, err := content.Archive()
		Expect(err).NotTo(HaveOccurred())

		ignitionBytes, err := io.ReadAll(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(ignitionBytes).To(Equal(ignitionArchiveBytes))
		Expect(len(ignitionBytes) % 4).To(Equal(0))
	})

	It("creates archive with system configs only", func() {
		systemConfig1 := []byte(`{"ignition": {"version": "3.1.0"}}`)
		systemConfig2 := []byte(`{"passwd": {"users": []}}`)

		content := IgnitionContent{
			SystemConfigs: map[string][]byte{
				"10-network.ign": systemConfig1,
				"20-users.ign":   systemConfig2,
			},
		}

		data, err := content.Archive()
		Expect(err).NotTo(HaveOccurred())

		archiveBytes, err := io.ReadAll(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(archiveBytes) % 4).To(Equal(0))

		files := extractCPIOFiles(archiveBytes)
		Expect(files).To(HaveLen(2))
		Expect(files["usr/lib/ignition/base.d/10-network.ign"]).To(Equal(systemConfig1))
		Expect(files["usr/lib/ignition/base.d/20-users.ign"]).To(Equal(systemConfig2))
	})

	It("creates archive with both main config and system configs", func() {
		systemConfig := []byte(`{"storage": {"files": []}}`)

		content := IgnitionContent{
			Config: ignitionContent,
			SystemConfigs: map[string][]byte{
				"30-storage.ign": systemConfig,
			},
		}

		data, err := content.Archive()
		Expect(err).NotTo(HaveOccurred())

		archiveBytes, err := io.ReadAll(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(archiveBytes) % 4).To(Equal(0))

		files := extractCPIOFiles(archiveBytes)
		Expect(files).To(HaveLen(2))
		Expect(files["config.ign"]).To(Equal(ignitionContent))
		Expect(files["usr/lib/ignition/base.d/30-storage.ign"]).To(Equal(systemConfig))
	})

	It("returns error when system config filename contains path separator", func() {
		content := IgnitionContent{
			SystemConfigs: map[string][]byte{
				"subdir/50-bad.ign": []byte("content"),
			},
		}

		_, err := content.Archive()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("contains path separators"))
		Expect(err.Error()).To(ContainSubstring("subdir/50-bad.ign"))
	})

	It("creates empty archive when no configs provided", func() {
		content := IgnitionContent{}

		data, err := content.Archive()
		Expect(err).NotTo(HaveOccurred())

		archiveBytes, err := io.ReadAll(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(archiveBytes) % 4).To(Equal(0))

		files := extractCPIOFiles(archiveBytes)
		Expect(files).To(HaveLen(0))
	})
})

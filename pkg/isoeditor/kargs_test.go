package isoeditor

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Context("kargs tests", func() {

	const (
		kargsConfileFile = `
{
  "default": "mitigations=auto,nosmt coreos.liveiso=fedora-coreos-35.20220103.3.0 ignition.firstboot ignition.platform.id=metal",
  "files": [
    {
      "offset": 970,
      "path": "EFI/fedora/grub.cfg"
    },
    {
      "offset": 1870,
      "path": "isolinux/isolinux.cfg"
    }
  ],
  "size": 1137
}
`
		kargsCentOSConfileFile = `
{
  "default": "mitigations=auto,nosmt coreos.liveiso=scos-413.9.20230103.0 ignition.firstboot ignition.platform.id=metal",
  "files": [
    {
      "offset": 970,
      "path": "EFI/centos/grub.cfg"
    },
    {
      "offset": 1870,
      "path": "isolinux/isolinux.cfg"
    }
  ],
  "size": 1137
}
`
		grubFileWithEmbedArea = `
function load_video {
  insmod efi_gop
}

insmod ext2

set timeout=5
### END /etc/grub.d/00_header ###

### BEGIN /etc/grub.d/10_linux ###
menuentry 'Fedora CoreOS (Live)' --class fedora --class gnu-linux --class gnu --class os {
	linux /images/pxeboot/vmlinuz mitigations=auto,nosmt coreos.liveiso=fedora-coreos-35.20220103.3.0 ignition.firstboot ignition.platform.id=metal
################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################ COREOS_KARG_EMBED_AREA
	initrd /images/pxeboot/initrd.img /images/ignition.img
}
`
		grubFileWithoutEmbedArea = `
function load_video {
  insmod efi_gop
}

insmod ext2

set timeout=5
### END /etc/grub.d/00_header ###

### BEGIN /etc/grub.d/10_linux ###
menuentry 'Fedora CoreOS (Live)' --class fedora --class gnu-linux --class gnu --class os {
	linux /images/pxeboot/vmlinuz mitigations=auto,nosmt coreos.liveiso=fedora-coreos-35.20220103.3.0 ignition.firstboot ignition.platform.id=metal
	initrd /images/pxeboot/initrd.img /images/ignition.img
}
`
	)

	mockFileReaderCreator := func(ret []byte, err error) FileReader {
		return func(_, _ string) ([]byte, error) {
			return ret, err
		}
	}
	mockFileReaderSuccess := func(fileData string) FileReader {
		return mockFileReaderCreator([]byte(fileData), nil)
	}
	mockFileReaderFailure := func() FileReader {
		return mockFileReaderCreator(nil, errors.New("this is an error"))
	}

	mockBoundariesFinderCreator := func(start, length int64, err error) BoundariesFinder {
		return func(_, _ string) (int64, int64, error) {
			return start, length, err
		}
	}

	mockBoundariesFinderSuccess := func(start, length int64) BoundariesFinder {
		return mockBoundariesFinderCreator(start, length, nil)
	}

	mockBoundariesFinderFailure := func() BoundariesFinder {
		return mockBoundariesFinderCreator(0, 0, errors.New("this is an error"))
	}

	Describe("kargsFiles", func() {
		It("fails to read kargs file", func() {
			files, err := kargsFiles("isoPath", mockFileReaderFailure())
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(Equal([]string{defaultGrubFilePath, defaultIsolinuxFilePath}))
		})
		It("kargs file is malformed", func() {
			files, err := kargsFiles("isoPath", mockFileReaderSuccess("malformedData"))
			Expect(err).To(HaveOccurred())
			Expect(files).To(BeNil())
		})
		It("empty kargs file", func() {
			fileData := `{"files": []}`
			files, err := kargsFiles("isoPath", mockFileReaderSuccess(fileData))
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(HaveLen(0))
		})
		It("non empty kargs file", func() {
			files, err := kargsFiles("isoPath", mockFileReaderSuccess(kargsConfileFile))
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(Equal([]string{"EFI/fedora/grub.cfg", "isolinux/isolinux.cfg"}))
		})
		It("works with centos", func() {
			files, err := kargsFiles("isoPath", mockFileReaderSuccess(kargsCentOSConfileFile))
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(Equal([]string{"EFI/centos/grub.cfg", "isolinux/isolinux.cfg"}))
		})
	})
	Describe("kargsEmbedAreaBoundariesFinder", func() {
		It("fail finding file boundaries", func() {
			_, _, err := kargsEmbedAreaBoundariesFinder("isoPath", "filePath", mockBoundariesFinderFailure(), mockFileReaderSuccess(grubFileWithEmbedArea))
			Expect(err).To(HaveOccurred())
		})
		It("fail reading file", func() {
			_, _, err := kargsEmbedAreaBoundariesFinder("isoPath", "filePath", mockBoundariesFinderSuccess(100, 100), mockFileReaderFailure())
			Expect(err).To(HaveOccurred())
		})
		It("no embed area found", func() {
			_, _, err := kargsEmbedAreaBoundariesFinder("isoPath", "filePath", mockBoundariesFinderSuccess(100, 100), mockFileReaderSuccess(grubFileWithoutEmbedArea))
			Expect(err).To(HaveOccurred())
		})
		It("embed area found", func() {
			start, length, err := kargsEmbedAreaBoundariesFinder("isoPath", "filePath", mockBoundariesFinderSuccess(1000, int64(len(grubFileWithEmbedArea))),
				mockFileReaderSuccess(grubFileWithEmbedArea))
			Expect(err).ToNot(HaveOccurred())
			Expect(start).To(Equal(int64(1375)))
			Expect(length).To(Equal(int64(1024)))
		})
	})
})

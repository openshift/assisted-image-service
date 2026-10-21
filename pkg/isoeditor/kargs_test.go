package isoeditor

import (
	"errors"
	"os"
	"path/filepath"

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

// Tests for EmbedKargsIntoBootImage
var _ = Describe("EmbedKargsIntoBootImage", func() {
	var (
		baseDir    string // acts as baseIsoPath (where /coreos/kargs.json is read from)
		stagingDir string // acts as stagingIsoPath (where files are written)
	)

	writeBaseKargsJSON := func(json string) {
		p := filepath.Join(baseDir, "coreos", "kargs.json")
		Expect(os.MkdirAll(filepath.Dir(p), 0755)).To(Succeed())
		Expect(os.WriteFile(p, []byte(json), 0644)).To(Succeed())
	}

	// helper to create a target file inside staging dir with a given size (filled with zeros)
	createStagingFile := func(rel string, size int) string {
		full := filepath.Join(stagingDir, rel)
		Expect(os.MkdirAll(filepath.Dir(full), 0755)).To(Succeed())
		buf := make([]byte, size)
		Expect(os.WriteFile(full, buf, 0644)).To(Succeed())
		return full
	}

	BeforeEach(func() {
		var err error
		baseDir, err = os.MkdirTemp("", "iso-base")
		Expect(err).ToNot(HaveOccurred())
		stagingDir, err = os.MkdirTemp("", "iso-staging")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
		os.RemoveAll(stagingDir)
	})

	It("fails when /coreos/kargs.json cannot be read from base ISO path", func() {
		// Do NOT create base coreos/kargs.json
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "any")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to read kargs config"))
	})

	It("fails when /coreos/kargs.json is malformed", func() {
		writeBaseKargsJSON(`{ not valid json }`)
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "newKargs")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse"))
	})

	It("fails when no kargs file entries are present", func() {
		writeBaseKargsJSON(`{"default":"abc","files":[],"size":10}`)
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "extra")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no kargs file entries"))
	})

	It("fails when a listed staging file does not exist", func() {
		writeBaseKargsJSON(`{
			"default": "abc",
			"files": [{"path":"cdboot.img","offset":10}],
			"size": 100
		}`)
		// Don't create cdboot.img in staging
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "zzz")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not exist"))
	})

	It("fails when kargs length exceeds configured Size", func() {
		// default=3 chars, custom=9 chars -> total 12 > size 10
		writeBaseKargsJSON(`{
			"default": "abc",
			"files": [{"path":"cdboot.img","offset":0}],
			"size": 10
		}`)
		_ = createStagingFile("cdboot.img", 32)
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "toolonggg")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds available field size"))
	})

	It("fails when size is not provided but available space (by file length) is insufficient", func() {
		// Size=0 means use file size heuristic:
		// file size 8, offset=4, default len=3 -> append offset = 7, remaining = 1
		// total needed default+custom = 3 + 3 = 6 > 1 -> error
		writeBaseKargsJSON(`{
			"default": "abc",
			"files": [{"path":"cdboot.img","offset":4}],
			"size": 0
		}`)
		_ = createStagingFile("cdboot.img", 8)
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "xyz")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds available field size"))
	})

	It("fails when the staging path exists but is a directory (open for write fails)", func() {
		writeBaseKargsJSON(`{
			"default": "abc",
			"files": [{"path":"cdboot.img","offset":5}],
			"size": 100
		}`)
		// Create a directory named cdboot.img
		Expect(os.MkdirAll(filepath.Join(stagingDir, "cdboot.img"), 0755)).To(Succeed())
		err := EmbedKargsIntoBootImage(baseDir, stagingDir, "ok")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to open target file"))
	})

	It("successfully embeds kargs into TWO different files with different offsets", func() {
		// default is "abc" (len=3)
		writeBaseKargsJSON(`{
			"default": "abc",
			"files": [
				{"path":"cdboot.img","offset":10},
				{"path":"coreos/kargs.json","offset":20}
			],
			"size": 1024
		}`)
		cdboot := createStagingFile("cdboot.img", 256)
		kargsBin := createStagingFile("coreos/kargs.json", 256)

		custom := "dual-file=ok"
		Expect(EmbedKargsIntoBootImage(baseDir, stagingDir, custom)).To(Succeed())

		// Verify writes at offset + len(default)
		cd, err := os.ReadFile(cdboot)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(cd[10+3 : 10+3+len(custom)])).To(Equal(custom))

		kb, err := os.ReadFile(kargsBin)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(kb[20+3 : 20+3+len(custom)])).To(Equal(custom))
	})
})

package isoeditor

import (
	"encoding/binary"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"

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
		// SeekToBlock sets the offset for the next read to the beginning of the 2048 bytes block.
		SeekToBlock := func(isoFD *os.File, block uint32) {
			_, err := isoFD.Seek(int64(block)*2048, 0)
			Expect(err).ToNot(HaveOccurred())
		}

		// ExtractElToritoBootImage extracts the El Torito boot image from an ISO file.
		ExtractElToritoBootImage := func(isoFile string) []byte {
			// Open the file, and remember to close it:
			isoFD, err := os.Open(isoFile)
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err = isoFD.Close()
				Expect(err).ToNot(HaveOccurred())
			}()

			// Verify the boot record:
			SeekToBlock(isoFD, 17)
			type BootRecord struct {
				VolDescType    byte
				StdIdentifier  [5]byte
				VolDescVersion byte
				BootSysId      [32]byte
				BootId         [32]byte
				BootCatalog    uint32
			}
			var bootRecord BootRecord
			err = binary.Read(isoFD, binary.LittleEndian, &bootRecord)
			Expect(err).ToNot(HaveOccurred())
			stdIdentifier := string(bootRecord.StdIdentifier[:])
			Expect(stdIdentifier).To(Equal("CD001"))
			bootSysId := strings.TrimRight(string(bootRecord.BootSysId[:]), "\x00")
			Expect(bootSysId).To(Equal("EL TORITO SPECIFICATION"))

			// Verify the boot catalog:
			SeekToBlock(isoFD, bootRecord.BootCatalog)
			type ValidationEntry struct {
				HeaderID   byte
				PlatformID byte
				Unused1    uint16
				IDString   [24]byte
				Checksum   uint16
				Key1       byte
				Key2       byte
			}
			var validationEntry ValidationEntry
			err = binary.Read(isoFD, binary.LittleEndian, &validationEntry)
			Expect(err).ToNot(HaveOccurred())
			Expect(validationEntry.HeaderID).To(Equal(byte(1)))
			Expect(validationEntry.Key1).To(Equal(byte(0x55)))
			Expect(validationEntry.Key2).To(Equal(byte(0xaa)))

			// Verify the initial entry:
			type InitialEntry struct {
				BootIndicator byte
				BootMediaType byte
				LoadSegment   uint16
				SystemType    byte
				Unused1       byte
				SectorCount   uint16
				Block         uint32
				Unused2       byte
			}
			var initialEntry InitialEntry
			err = binary.Read(isoFD, binary.LittleEndian, &initialEntry)
			Expect(err).ToNot(HaveOccurred())
			Expect(initialEntry.BootIndicator).To(Equal(byte(0x88)))
			Expect(initialEntry.BootMediaType).To(BeZero())

			// Extract the image:
			bootImageSize := int(initialEntry.SectorCount) * 512
			bootImageBytes := make([]byte, bootImageSize)
			SeekToBlock(isoFD, initialEntry.Block)
			n, err := isoFD.Read(bootImageBytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(n).To(Equal(bootImageSize))
			return bootImageBytes
		}

		It("generates an iso with the content in the given directory", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			isoPath := filepath.Join(dir, "test.iso")

			Expect(Create(isoPath, filesDir, "my-vol")).To(Succeed())

			d, err := diskfs.Open(isoPath, diskfs.WithOpenMode(diskfs.ReadOnly))
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

		It("generates an iso - single boot file (efi)", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			isoPath := filepath.Join(dir, "test.iso")
			Expect(os.Remove(filepath.Join(filesDir, "isolinux/isolinux.bin"))).To(Succeed())

			haveBootFiles, err := haveBootFiles(filesDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeFalse())

			Expect(os.WriteFile(filepath.Join(filesDir, "boot.catalog"), []byte(""), 0600)).To(Succeed())
			Expect(Create(isoPath, filesDir, "my-vol")).To(Succeed())
		})

		It("generates an iso - single boot file, missing catalog file", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			isoPath := filepath.Join(dir, "test.iso")
			Expect(os.Remove(filepath.Join(filesDir, "isolinux/isolinux.bin"))).To(Succeed())

			haveBootFiles, err := haveBootFiles(filesDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeFalse())

			err = Create(isoPath, filesDir, "my-vol")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("missing boot.catalog file"))
		})

		It("generates an iso - no boot files", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			isoPath := filepath.Join(dir, "test.iso")
			Expect(os.Remove(filepath.Join(filesDir, "isolinux/isolinux.bin"))).To(Succeed())
			Expect(os.Remove(filepath.Join(filesDir, "images/efiboot.img"))).To(Succeed())

			haveBootFiles, err := haveBootFiles(filesDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeFalse())

			Expect(Create(isoPath, filesDir, "my-vol")).To(Succeed())
		})

		It("Preserves the El Torito boot image for s390x", func() {
			// Create the input ISO:
			var err error
			inputDir, inputFile := createS390TestFiles("input", 0)
			defer func() {
				err = os.RemoveAll(inputDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.Remove(inputFile)
				Expect(err).ToNot(HaveOccurred())
			}()

			// Read the input boot image:
			inputBootImageFile := filepath.Join(inputDir, "images", "cdboot.img")
			inputBootImageBytes, err := os.ReadFile(inputBootImageFile)
			Expect(err).ToNot(HaveOccurred())
			inputBootImageSize := len(inputBootImageBytes)

			// Create the output ISO:
			outputDir, err := os.MkdirTemp("", "*.test")
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err = os.RemoveAll(outputDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			outputFile := filepath.Join(outputDir, "output.iso")
			err = Create(outputFile, inputDir, "output")
			Expect(err).ToNot(HaveOccurred())

			// Read the output boot image and verify that is equal to the input. Note
			// that the image written to disk may be larger than the input because of
			// padding, so we need to take that into account.
			outputBootImageBytes := ExtractElToritoBootImage(outputFile)
			outputBootImageSize := len(outputBootImageBytes)
			Expect(outputBootImageSize).To(BeNumerically(">=", inputBootImageSize))
			Expect(outputBootImageBytes[0:inputBootImageSize]).To(Equal(inputBootImageBytes))
		})

		It("Round the El Torito boot image for s390x to a multiple of four 512 bytes sectors", func() {
			// Create the input ISO:
			var err error
			inputDir, inputFile := createS390TestFiles("input", 1*1024*1024-512)
			defer func() {
				err = os.RemoveAll(inputDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.Remove(inputFile)
				Expect(err).ToNot(HaveOccurred())
			}()

			// Create the output ISO:
			outputDir, err := os.MkdirTemp("", "*.test")
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err = os.RemoveAll(outputDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			outputFile := filepath.Join(outputDir, "output.iso")
			err = Create(outputFile, inputDir, "output")
			Expect(err).ToNot(HaveOccurred())

			// Read the output boot image and verify that it has been truncated:
			outputBootImageBytes := ExtractElToritoBootImage(outputFile)
			outputBootImageSize := len(outputBootImageBytes)
			Expect(outputBootImageSize % 2048).To(BeZero())
		})

		It("Truncates the El Torito boot image for s390x to 65535 sectors", func() {
			// Note that doing this is arguable an error, but it is what the 'xorrisofs'
			// tool used to generate the release ISO files does, and we want to do the
			//same.

			// Create the input ISO:
			var err error
			inputDir, inputFile := createS390TestFiles("input", 64*1024*1024)
			defer func() {
				err = os.RemoveAll(inputDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.Remove(inputFile)
				Expect(err).ToNot(HaveOccurred())
			}()

			// Create the output ISO:
			outputDir, err := os.MkdirTemp("", "*.test")
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err = os.RemoveAll(outputDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			outputFile := filepath.Join(outputDir, "output.iso")
			err = Create(outputFile, inputDir, "output")
			Expect(err).ToNot(HaveOccurred())

			// Read the output boot image and verify that it has been truncated:
			outputBootImageBytes := ExtractElToritoBootImage(outputFile)
			outputBootImageSize := len(outputBootImageBytes)
			Expect(outputBootImageSize).To(Equal(math.MaxUint16 * 512))
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

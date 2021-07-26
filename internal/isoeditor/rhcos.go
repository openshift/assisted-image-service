package isoeditor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"

	"github.com/carbonin/assisted-image-service/internal/isoutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	RamDiskPaddingLength  = uint64(1024 * 1024) // 1MB
	IgnitionPaddingLength = uint64(256 * 1024)  // 256KB
	ignitionImagePath     = "/images/ignition.img"
	ramDiskImagePath      = "/images/assisted_installer_custom.img"
	ignitionHeaderKey     = "coreiso+"
	ramdiskHeaderKey      = "ramdisk+"
)

type OffsetInfo struct {
	Key    [8]byte
	Offset uint64
	Length uint64
}

//go:generate mockgen -package=isoeditor -destination=mock_editor.go -self_package=github.com/openshift/assisted-service/internal/isoeditor . Editor
type Editor interface {
	CreateMinimalISOTemplate(rootFSURL string) (string, error)
}

type rhcosEditor struct {
	isoHandler isoutil.Handler
	log        logrus.FieldLogger
	workDir    string
}

// Creates the template minimal iso by removing the rootfs and adding the url
// Returns the path to the created iso file
func (e *rhcosEditor) CreateMinimalISOTemplate(rootFSURL string) (string, error) {
	if err := e.isoHandler.Extract(); err != nil {
		return "", err
	}

	if err := os.Remove(e.isoHandler.ExtractedPath("images/pxeboot/rootfs.img")); err != nil {
		return "", err
	}

	if err := e.embedInitrdPlaceholders(); err != nil {
		e.log.WithError(err).Warnf("Failed to embed initrd placeholders")
		return "", err
	}

	if err := e.fixTemplateConfigs(rootFSURL); err != nil {
		e.log.WithError(err).Warnf("Failed to edit template configs")
		return "", err
	}

	e.log.Info("Creating minimal ISO template")
	isoPath, err := e.create()
	if err != nil {
		e.log.WithError(err).Errorf("Failed to minimal create ISO template")
		return "", err
	}

	if err := e.embedOffsetsInSystemArea(isoPath); err != nil {
		e.log.WithError(err).Errorf("Failed to embed offsets in ISO system area")
		return "", err
	}

	return isoPath, nil
}

func (e *rhcosEditor) embedInitrdPlaceholders() error {
	// Create ramdisk image placeholder
	if err := e.createImagePlaceholder(ramDiskImagePath, RamDiskPaddingLength); err != nil {
		return errors.Wrap(err, "Failed to create placeholder for custom ramdisk image")
	}

	return nil
}

func (e *rhcosEditor) embedOffsetsInSystemArea(isoPath string) error {
	ignitionOffset, err := isoutil.GetFileLocation(ignitionImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ignition image offset")
	}

	ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ram disk image offset")
	}

	ignitionSize, err := isoutil.GetFileSize(ignitionImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ignition image size")
	}

	ramDiskSize, err := isoutil.GetFileSize(ramDiskImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ram disk image size")
	}

	var ignitionOffsetInfo OffsetInfo
	copy(ignitionOffsetInfo.Key[:], ignitionHeaderKey)
	ignitionOffsetInfo.Offset = ignitionOffset
	ignitionOffsetInfo.Length = ignitionSize

	var ramDiskOffsetInfo OffsetInfo
	copy(ramDiskOffsetInfo.Key[:], ramdiskHeaderKey)
	ramDiskOffsetInfo.Offset = ramDiskOffset
	ramDiskOffsetInfo.Length = ramDiskSize

	return writeHeader(&ignitionOffsetInfo, &ramDiskOffsetInfo, isoPath)
}

func (e *rhcosEditor) createImagePlaceholder(imagePath string, paddingLength uint64) error {
	f, err := os.Create(e.isoHandler.ExtractedPath(imagePath))
	if err != nil {
		return err
	}
	defer f.Close()

	err = f.Truncate(int64(paddingLength))
	if err != nil {
		return err
	}

	return nil
}

func (e *rhcosEditor) create() (string, error) {
	isoPath, err := tempFileName(e.workDir)
	if err != nil {
		return "", err
	}

	volumeID, err := e.isoHandler.VolumeIdentifier()
	if err != nil {
		return "", err
	}
	if err = e.isoHandler.Create(isoPath, volumeID); err != nil {
		return "", err
	}

	return isoPath, nil
}

func (e *rhcosEditor) fixTemplateConfigs(rootFSURL string) error {
	// Add the rootfs url
	replacement := fmt.Sprintf("$1 $2 'coreos.live.rootfs_url=%s'", rootFSURL)
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+linux) (.+| )+$`, replacement); err != nil {
		return err
	}
	replacement = fmt.Sprintf("$1 $2 coreos.live.rootfs_url=%s", rootFSURL)
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append) (.+| )+$`, replacement); err != nil {
		return err
	}

	// Remove the coreos.liveiso parameter
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), ` coreos.liveiso=\S+`, ""); err != nil {
		return err
	}
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), ` coreos.liveiso=\S+`, ""); err != nil {
		return err
	}

	// Edit config to add custom ramdisk image to initrd
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+initrd) (.+| )+$`, fmt.Sprintf("$1 $2 %s", ramDiskImagePath)); err != nil {
		return err
	}
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append.*initrd=\S+) (.*)$`, fmt.Sprintf("${1},%s ${2}", ramDiskImagePath)); err != nil {
		return err
	}

	return nil
}

func editFile(fileName string, reString string, replacement string) error {
	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(reString)
	newContent := re.ReplaceAllString(string(content), replacement)

	if err := ioutil.WriteFile(fileName, []byte(newContent), 0600); err != nil {
		return err
	}

	return nil
}

func tempFileName(baseDir string) (string, error) {
	f, err := ioutil.TempFile(baseDir, "isoeditor")
	if err != nil {
		return "", err
	}
	path := f.Name()

	if err := os.Remove(path); err != nil {
		return "", err
	}

	return path, nil
}

// Writing the offsets of initrd images in the end of system area (first 32KB).
// As the ISO template is generated by us, we know that this area should be empty.
func writeHeader(ignitionOffsetInfo, ramDiskOffsetInfo *OffsetInfo, isoPath string) error {
	iso, err := os.OpenFile(isoPath, os.O_WRONLY, 0o664)
	if err != nil {
		return err
	}
	defer iso.Close()

	// Starting to write from the end of the system area in order to easily support
	// additional offsets (and as done in coreos-assembler/src/cmd-buildextend-live)
	headerEndOffset := int64(32768)

	// Write ignition config
	writtenBytesLength, err := writeOffsetInfo(headerEndOffset, ignitionOffsetInfo, iso)
	if err != nil {
		return err
	}

	// Write ram disk
	_, err = writeOffsetInfo(headerEndOffset-writtenBytesLength, ramDiskOffsetInfo, iso)
	if err != nil {
		return err
	}

	return nil
}

func writeOffsetInfo(headerOffset int64, offsetInfo *OffsetInfo, iso *os.File) (int64, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, offsetInfo)
	if err != nil {
		return 0, err
	}

	bytesLength := int64(buf.Len())
	headerOffset = headerOffset - bytesLength
	_, err = iso.Seek(headerOffset, io.SeekStart)
	if err != nil {
		return 0, err
	}
	_, err = iso.Write(buf.Bytes())
	if err != nil {
		return 0, err
	}

	return bytesLength, nil
}

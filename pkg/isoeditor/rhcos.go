package isoeditor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/openshift/assisted-image-service/internal/common"
	log "github.com/sirupsen/logrus"
)

const (
	RamDiskPaddingLength        = uint64(1024 * 1024) // 1MB
	NmstatectlPathInRamdisk     = "/usr/bin/nmstatectl"
	ramDiskImagePath            = "/images/assisted_installer_custom.img"
	nmstateDiskImagePath        = "/images/nmstate.img"
	MinimalVersionForNmstatectl = "4.18.0-ec.0"
	RootfsImagePath             = "images/pxeboot/rootfs.img"
)

//go:generate mockgen -package=isoeditor -destination=mock_editor.go . Editor
type Editor interface {
	CreateMinimalISOTemplate(fullISOPath, rootFSURL, arch, minimalISOPath, openshiftVersion, nmstatectlPath string) error
}

type rhcosEditor struct {
	workDir        string
	nmstateHandler NmstateHandler
}

func NewEditor(dataDir string, nmstateHandler NmstateHandler) Editor {
	return &rhcosEditor{
		workDir:        dataDir,
		nmstateHandler: nmstateHandler,
	}
}

// CreateMinimalISO Creates the minimal iso by removing the rootfs and adding the url
func CreateMinimalISO(extractDir, volumeID, rootFSURL, arch, minimalISOPath string) error {
	if err := os.Remove(filepath.Join(extractDir, RootfsImagePath)); err != nil {
		return err
	}

	if err := embedInitrdPlaceholders(extractDir); err != nil {
		log.WithError(err).Warnf("Failed to embed initrd placeholders")
		return err
	}

	var includeNmstateRamDisk bool
	if _, err := os.Stat(filepath.Join(extractDir, nmstateDiskImagePath)); err == nil {
		includeNmstateRamDisk = true
	}

	if err := fixGrubConfig(rootFSURL, extractDir, includeNmstateRamDisk); err != nil {
		log.WithError(err).Warnf("Failed to edit grub config")
		return err
	}

	// ignore isolinux.cfg for ppc64le because it doesn't exist
	if arch != "ppc64le" {
		if err := fixIsolinuxConfig(rootFSURL, extractDir, includeNmstateRamDisk); err != nil {
 			log.WithError(err).Warnf("Failed to edit isolinux config")
 			return err
 		}
	}

	if err := Create(minimalISOPath, extractDir, volumeID); err != nil {
		return err
	}
	return nil
}

// CreateMinimalISOTemplate Creates the template minimal iso by removing the rootfs and adding the url
func (e *rhcosEditor) CreateMinimalISOTemplate(fullISOPath, rootFSURL, arch, minimalISOPath, openshiftVersion, nmstatectlPath string) error {
	extractDir, err := os.MkdirTemp(e.workDir, "isoutil")
	if err != nil {
		return err
	}

	if err = Extract(fullISOPath, extractDir); err != nil {
		return err
	}

	volumeID, err := VolumeIdentifier(fullISOPath)
	if err != nil {
		return err
	}

	ramDiskPath := filepath.Join(extractDir, nmstateDiskImagePath)

	versionOK, err := common.VersionGreaterOrEqual(openshiftVersion, MinimalVersionForNmstatectl)
	if err != nil {
		return err
	}

	if versionOK {
		var compressedCpio []byte
		var readErr error

		if _, err = os.Stat(nmstatectlPath); err == nil {
			// Read and return the cached content
			compressedCpio, readErr = os.ReadFile(nmstatectlPath)
			if readErr != nil {
				return fmt.Errorf("failed to read cached nmstatectl: %v", readErr)
			}
		} else if os.IsNotExist(err) {
			// File doesn't exist - this should be an error condition
			return fmt.Errorf("nmstatectl cache file not found: %s", nmstatectlPath)
		} else {
			// Some other error occurred
			return fmt.Errorf("failed to stat nmstatectl cache file: %v", err)
		}

		err = os.WriteFile(ramDiskPath, compressedCpio, 0755) //nolint:gosec
		if err != nil {
			return err
		}
	}

	err = CreateMinimalISO(extractDir, volumeID, rootFSURL, arch, minimalISOPath)
	if err != nil {
		return err
	}

	return nil
}

func embedInitrdPlaceholders(extractDir string) error {
	f, err := os.Create(filepath.Join(extractDir, ramDiskImagePath))
	if err != nil {
		return err
	}
	defer func() {
		if deferErr := f.Sync(); deferErr != nil {
			log.WithError(deferErr).Error("Failed to sync disk image placeholder file")
		}
		if deferErr := f.Close(); deferErr != nil {
			log.WithError(deferErr).Error("Failed to close disk image placeholder file")
		}
	}()

	err = f.Truncate(int64(RamDiskPaddingLength))
	if err != nil {
		return err
	}

	return nil
}

func fixGrubConfig(rootFSURL, extractDir string, includeNmstateRamDisk bool) error {
	availableGrubPaths := []string{"EFI/redhat/grub.cfg", "EFI/fedora/grub.cfg", "boot/grub/grub.cfg", "EFI/centos/grub.cfg"}
	var foundGrubPath string
	for _, pathSection := range availableGrubPaths {
		path := filepath.Join(extractDir, pathSection)
		if _, err := os.Stat(path); err == nil {
			foundGrubPath = path
			break
		}
	}
	if len(foundGrubPath) == 0 {
		return fmt.Errorf("no grub.cfg found, possible paths are %v", availableGrubPaths)
	}

	// Add the rootfs url
	replacement := fmt.Sprintf("$1 $2 'coreos.live.rootfs_url=%s'", rootFSURL)
	grubFileContent, err := replacePatternInFile(foundGrubPath, `(?m)^(\s+linux) (.+| )+$`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(foundGrubPath, grubFileContent); err != nil {
		return err
	}

	// Remove the coreos.liveiso parameter
	replacement= ""
	grubFileContent, err = replacePatternInFile(foundGrubPath, ` coreos.liveiso=\S+`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(foundGrubPath, grubFileContent); err != nil {
		return err
	}

	// Edit config to add custom ramdisk image to initrd
	replacement = fmt.Sprintf("$1 $2 %s", ramDiskImagePath)
	if includeNmstateRamDisk {
		replacement = fmt.Sprintf("$1 $2 %s %s", ramDiskImagePath, nmstateDiskImagePath)
	}
	contentWithRamDiskPath, err := replacePatternInFile(foundGrubPath, `(?m)^(\s+initrd) (.+| )+$`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(foundGrubPath, contentWithRamDiskPath); err != nil {
		return err
	}

	return nil
}

func fixIsolinuxConfig(rootFSURL, extractDir string, includeNmstateRamDisk bool) error {
	isolinuxFile := filepath.Join(extractDir, "isolinux/isolinux.cfg")
	replacement := fmt.Sprintf("$1 $2 coreos.live.rootfs_url=%s", rootFSURL)

	isolinuxFileContent, err := replacePatternInFile(isolinuxFile, `(?m)^(\s+append) (.+| )+$`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(isolinuxFile, isolinuxFileContent); err != nil {
		return err
	}

	// Remove the coreos.liveiso parameter
	replacement= ""
	isolinuxFileContent, err = replacePatternInFile(isolinuxFile, ` coreos.liveiso=\S+`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(isolinuxFile, isolinuxFileContent); err != nil {
		return err
	}

	replacement = fmt.Sprintf("${1},%s ${2}", ramDiskImagePath)
	if includeNmstateRamDisk {
		replacement = fmt.Sprintf("${1},%s,%s ${2}", ramDiskImagePath, nmstateDiskImagePath)
	}
	contentWithRamDiskPath, err := replacePatternInFile(isolinuxFile, `(?m)^(\s+append.*initrd=\S+) (.*)$`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(isolinuxFile, contentWithRamDiskPath); err != nil {
		return err
 	}

	return nil
}

// replacePatternInFile reads the file, applies a regex substitution, and returns the modified content.
func replacePatternInFile(fileName, reString, replacement string) (string, error) {
	data, err := os.ReadFile(fileName)
 	if err != nil {
		return "", err
 	}
 
 	re := regexp.MustCompile(reString)
	content := re.ReplaceAllString(string(data), replacement)
	return content, nil 
}

// saveFile writes the given content to the specified file with 0600 permissions.
func saveFile(fileName, content string) error {
	if err := os.WriteFile(fileName, []byte(content), 0600); err != nil {
 		return err
 	}
 	return nil
 }
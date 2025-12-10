package isoeditor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// transformKernelArgs applies the standard kernel argument transformations:
// 1. Remove coreos.liveiso parameter
// 2. Add coreos.live.rootfs_url parameter at the specified insertion point
func transformKernelArgs(content string, insertionPattern string, rootFSURL string) (string, error) {
	// Validate rootfs URL
	if strings.Contains(rootFSURL, "$") {
		return "", fmt.Errorf("invalid rootfs URL: contains invalid character '$'")
	}
	if strings.Contains(rootFSURL, "\\") {
		return "", fmt.Errorf("invalid rootfs URL: contains invalid character '\\'")
	}

	var err error

	// Remove the coreos.liveiso parameter
	content, err = editString(content, `\b(?P<replace>coreos\.liveiso=\S+ ?)`, "")
	if err != nil {
		return "", err
	}

	// Add the rootfs_url parameter at the specified insertion point
	replacement := " coreos.live.rootfs_url=\"" + rootFSURL + "\""
	content, err = editString(content, insertionPattern, replacement)
	if err != nil {
		return "", err
	}

	return content, nil
}

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

	// Read the file content
	content, err := os.ReadFile(foundGrubPath)
	if err != nil {
		return err
	}
	contentStr := string(content)

	// Typical grub.cfg lines we're modifying:
	//
	//	linux /images/pxeboot/vmlinuz rw  coreos.liveiso=rhcos-9.6.20250523-0 ignition.firstboot ignition.platform.id=metal
	//	initrd /images/pxeboot/initrd.img /images/ignition.img

	// Apply standard kernel argument transformations (remove coreos.liveiso, add rootfs_url)
	contentStr, err = transformKernelArgs(contentStr, `(?m)^(\s+linux .+)(?P<replace>$)`, rootFSURL)
	if err != nil {
		return err
	}

	// Edit config to add custom ramdisk image to initrd - capture the end-of-line position to append our images
	var initrdReplacement string
	if includeNmstateRamDisk {
		initrdReplacement = " " + ramDiskImagePath + " " + nmstateDiskImagePath
	} else {
		initrdReplacement = " " + ramDiskImagePath
	}
	contentStr, err = editString(contentStr, `(?m)^(\s+initrd .+)(?P<replace>$)`, initrdReplacement)
	if err != nil {
		return err
	}

	// Write the modified content back to the file
	return os.WriteFile(foundGrubPath, []byte(contentStr), 0600)
}

func fixIsolinuxConfig(rootFSURL, extractDir string, includeNmstateRamDisk bool) error {
	isolinuxPath := filepath.Join(extractDir, "isolinux/isolinux.cfg")

	// Read the file content
	content, err := os.ReadFile(isolinuxPath)
	if err != nil {
		return err
	}
	contentStr := string(content)

	// Typical isolinux.cfg line we're modifying:
	//
	//	append initrd=/images/pxeboot/initrd.img,/images/ignition.img rw  coreos.liveiso=rhcos-9.6.20250523-0 ignition.firstboot ignition.platform.id=metal

	// Apply standard kernel argument transformations (remove coreos.liveiso, add rootfs_url)
	contentStr, err = transformKernelArgs(contentStr, `(?m)^(\s+append .+)(?P<replace>$)`, rootFSURL)
	if err != nil {
		return err
	}

	// Add ramdisk images to initrd specification - capture the position right after the initrd argument to append our images
	var initrdReplacement string
	if includeNmstateRamDisk {
		initrdReplacement = "," + ramDiskImagePath + "," + nmstateDiskImagePath
	} else {
		initrdReplacement = "," + ramDiskImagePath
	}
	contentStr, err = editString(contentStr, `(?m)^(\s+append.*initrd=\S+)(?P<replace>)`, initrdReplacement)
	if err != nil {
		return err
	}

	// Write the modified content back to the file
	return os.WriteFile(isolinuxPath, []byte(contentStr), 0600)
}

// editString applies a regex replacement to a string and returns the modified string
// It looks for a named capture group called "replace" and replaces only that content, using precise string manipulation
func editString(content string, reString string, replacement string) (string, error) {
	re := regexp.MustCompile(reString)

	// Get the index of the "replace" named capture group
	replaceIndex := re.SubexpIndex("replace")
	if replaceIndex == -1 {
		return "", fmt.Errorf("regex must have a named capture group called 'replace'")
	}

	// Find the first match with subgroups
	submatchIndexes := re.FindStringSubmatchIndex(content)
	if submatchIndexes == nil {
		// No match found
		return content, nil
	}

	// submatchIndexes contains [fullMatchStart, fullMatchEnd, group1Start, group1End, group2Start, group2End, ...]
	if len(submatchIndexes) < (replaceIndex+1)*2 {
		return "", fmt.Errorf("regex match does not contain the 'replace' capture group")
	}

	replaceStart := submatchIndexes[replaceIndex*2]
	replaceEnd := submatchIndexes[replaceIndex*2+1]

	// Replace only the "replace" capturing group
	newContent := content[:replaceStart] + replacement + content[replaceEnd:]

	return newContent, nil
}

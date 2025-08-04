package isoeditor

import (
	"encoding/json"
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

type FileEntry struct {
	Offset int    `json:"offset"`
	Path   string `json:"path"`
	End    int    `json:"end"`
	Pad    int    `json:"pad"`
}

type KargsConfig struct {
	Default string      `json:"default"`
	Files   []FileEntry `json:"files"`
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
		contentWithRamDiskPath, err := fixIsolinuxConfig(rootFSURL, extractDir, includeNmstateRamDisk)
		if err != nil {
			log.WithError(err).Warnf("Failed to edit isolinux config")
			return err
		}
		if err := fixKargsConfig(extractDir, contentWithRamDiskPath, includeNmstateRamDisk); err != nil {
			log.WithError(err).Warnf("Failed to edit kargs config")
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

	data, err := os.ReadFile(foundGrubPath)
	if err != nil {
		return err
	}
	content := string(data)

	// Add the rootfs url
	replacement := fmt.Sprintf("$1 $2 'coreos.live.rootfs_url=%s'", rootFSURL)
	content, err = replace(content, `(?m)^(\s+linux) (.+| )+$`, replacement)
	if err != nil {
		return err
	}

	// Remove the coreos.liveiso parameter
	replacement = ""
	content, err = replace(content, ` coreos.liveiso=\S+`, replacement)
	if err != nil {
		return err
	}

	// Edit config to add custom ramdisk image to initrd
	replacement = fmt.Sprintf("$1 $2 %s", ramDiskImagePath)
	if includeNmstateRamDisk {
		replacement = fmt.Sprintf("$1 $2 %s %s", ramDiskImagePath, nmstateDiskImagePath)
	}
	content, err = replace(content, `(?m)^(\s+initrd) (.+| )+$`, replacement)
	if err != nil {
		return err
	}
	if err := saveFile(foundGrubPath, content); err != nil {
		return err
	}

	return nil
}

func fixIsolinuxConfig(rootFSURL, extractDir string, includeNmstateRamDisk bool) (string, error) {
	isolinuxFile := filepath.Join(extractDir, "isolinux/isolinux.cfg")
	data, err := os.ReadFile(isolinuxFile)
	if err != nil {
		return "", err
	}
	content := string(data)
	replacement := fmt.Sprintf("$1 $2 coreos.live.rootfs_url=%s", rootFSURL)

	content, err = replace(content, `(?m)^(\s+append) (.+| )+$`, replacement)
	if err != nil {
		return "", err
	}

	// Remove the coreos.liveiso parameter
	replacement = ""
	content, err = replace(content, ` coreos.liveiso=\S+`, replacement)
	if err != nil {
		return "", err
	}

	replacement = fmt.Sprintf("${1},%s ${2}", ramDiskImagePath)
	if includeNmstateRamDisk {
		replacement = fmt.Sprintf("${1},%s,%s ${2}", ramDiskImagePath, nmstateDiskImagePath)
	}
	content, err = replace(content, `(?m)^(\s+append.*initrd=\S+) (.*)$`, replacement)
	if err != nil {
		return "", err
	}
	if err := saveFile(isolinuxFile, content); err != nil {
		return "", err
	}

	return content, nil
}

func fixKargsConfig(extractDir, contentWithRamDiskPath string, includeNmstateRamDisk bool) error {
	kernelArgs, err := extractBootArgs(contentWithRamDiskPath)
	if err != nil {
		return err
	}
	offset := 1903
	if includeNmstateRamDisk {
		offset = 1923
	}

	kargsFile := filepath.Join(extractDir, "/coreos/kargs.jso")
	kargsContent, err := buildKargsContent(kargsFile, kernelArgs, offset)
	if err != nil {
		return err
	}

	if err := saveFile(kargsFile, kargsContent); err != nil {
		return err
	}
	return nil
}

func replace(data, reString, replacement string) (string, error) {
	re := regexp.MustCompile(reString)
	content := re.ReplaceAllString(data, replacement)
	return content, nil
}

// saveFile writes the given content to the specified file with 0600 permissions.
func saveFile(fileName, content string) error {
	if err := os.WriteFile(fileName, []byte(content), 0600); err != nil {
		return err
	}
	return nil
}

// extractBootArgs parses isolinux.cfg content to
// extract kernel arguments starting from 'rw'.
// It skips lines after the COREOS_KARG_EMBED_AREA marker and
// returns the args from the first 'append' line found.
func extractBootArgs(isolinuxFileContent string) (string, error) {
	lines := strings.Split(isolinuxFileContent, "\n")
	for _, line := range lines {
		if strings.Contains(line, "COREOS_KARG_EMBED_AREA") {
			break
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "append ") {
			// Extract the line content after "append "
			kernelArgs := strings.TrimPrefix(trimmed, "append ")
			// Find " rw " (with spaces to avoid partial matches)
			index := strings.Index(kernelArgs, " rw ")
			if index == -1 {
				index = strings.Index(kernelArgs, " rw") // fallback if it's at end
			}
			if index != -1 {
				return kernelArgs[index+1:], nil // return starting from "rw ..."
			}
		}
	}
	return "", fmt.Errorf("could not find line starting with 'append' and containing 'rw'")
}

// buildKargsContent reads the kargs.jso file, updates the "default" kernel args
// , offset, and returns the modified JSON as a string.
func buildKargsContent(kargsFile, content string, offset int) (string, error) {
	var cfg KargsConfig

	data, err := os.ReadFile(kargsFile)
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}

	cfg.Default = content
	for i := range cfg.Files {
		if cfg.Files[i].Path == "isolinux/isolinux.cfg" {
			cfg.Files[i].Offset = offset
		}
	}

	updatedJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}

	return string(updatedJSON), nil
}

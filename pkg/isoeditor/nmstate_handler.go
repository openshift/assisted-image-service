package isoeditor

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

//go:generate mockgen -package=isoeditor -destination=mock_nmstate_handler.go . NmstateHandler
type NmstateHandler interface {
	CreateNmstateRamDisk(nmstatectlPath, ramDiskPath string) error
	ExtractNmstatectl(rootfsPath string) (string, error)
}

type nmstateHandler struct {
	workDir, nmstateDir string
}

func NewNmstateHandler(workDir string) NmstateHandler {
	return &nmstateHandler{
		workDir: workDir,
	}
}

func (n *nmstateHandler) CreateNmstateRamDisk(nmstatectlPath, ramDiskPath string) error {
	// Check if nmstatectl binary file exists
	if _, err := os.Stat(nmstatectlPath); os.IsNotExist(err) {
		return err
	}

	// Read binary
	nmstateBinContent, err := os.ReadFile(nmstatectlPath)
	if err != nil {
		return err
	}

	// Create a compressed RAM disk image with the nmstatectl binary
	compressedCpio, err := generateCompressedCPIO(nmstateBinContent, NmstatectlPathInRamdisk, 0o100_755)
	if err != nil {
		return err
	}

	// Write RAM disk file
	err = os.WriteFile(ramDiskPath, compressedCpio, 0755) //nolint:gosec
	if err != nil {
		return err
	}

	// Remove temp dir
	if err := os.RemoveAll(n.nmstateDir); err != nil {
		return err
	}

	return nil
}

func (n *nmstateHandler) ExtractNmstatectl(rootfsPath string) (string, error) {
	var err error
	n.nmstateDir, err = os.MkdirTemp(n.workDir, "nmstate")
	if err != nil {
		return "", err
	}
	_, err = execute(fmt.Sprintf("7z x %s", rootfsPath), n.nmstateDir)
	if err != nil {
		log.Errorf("failed to 7z x rootfs.img: %v", err.Error())
		return "", err
	}
	// limiting files is needed on el<=9 due to https://github.com/plougher/squashfs-tools/issues/125
	ulimit := "ulimit -n 1024"
	list, err := execute(fmt.Sprintf("%s ; unsquashfs -d '' -lc %s", ulimit, "root.squashfs"), n.nmstateDir)
	if err != nil {
		log.Errorf("failed to unsquashfs root.squashfs: %v", err.Error())
		return "", err
	}

	r, err := regexp.Compile(".*nmstatectl")
	if err != nil {
		log.Errorf("failed to compile regexp: %v", err.Error())
		return "", err
	}
	binaryPath := r.FindString(list)
	if err != nil {
		log.Errorf("failed to compile regexp: %v", err.Error())
		return "", err
	}
	_, err = execute(fmt.Sprintf("%s ; unsquashfs -no-xattrs %s -extract-file %s", ulimit, "root.squashfs", binaryPath), n.nmstateDir)
	if err != nil {
		log.Errorf("failed to unsquashfs root.squashfs: %v", err.Error())
		return "", err
	}
	return filepath.Join(n.nmstateDir, "squashfs-root", binaryPath), nil
}

func execute(command, workDir string) (string, error) {
	var stdoutBytes, stderrBytes bytes.Buffer
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = &stdoutBytes
	cmd.Stderr = &stderrBytes
	log.Infof(fmt.Sprintf("Running cmd: %s\n", command))
	cmd.Dir = workDir
	err := cmd.Run()
	if err != nil {
		return "", errors.Wrapf(err, "Failed to execute cmd (%s): %s", cmd, stderrBytes.String())
	}

	return strings.TrimSuffix(stdoutBytes.String(), "\n"), nil
}

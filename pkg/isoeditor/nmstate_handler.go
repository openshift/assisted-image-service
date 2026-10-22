package isoeditor

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/erofs"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

//go:generate mockgen -package=isoeditor -destination=mock_nmstate_handler.go . NmstateHandler
type NmstateHandler interface {
	BuildNmstateCpioArchive(rootfsPath string) ([]byte, error)
}

type nmstateHandler struct {
	workDir                    string
	executer                   Executer
	nmstatectlExtractorFactory NmstatectlExtractorFactory
}

func NewNmstateHandler(workDir string, executer Executer, nmstatectlExtractorFactory NmstatectlExtractorFactory) NmstateHandler {
	return &nmstateHandler{
		workDir:                    workDir,
		executer:                   executer,
		nmstatectlExtractorFactory: nmstatectlExtractorFactory,
	}
}

func (n *nmstateHandler) BuildNmstateCpioArchive(rootfsPath string) ([]byte, error) {
	// Extract nmstatectl binary
	var err error
	nmstateDir := filepath.Join(n.workDir, "nmstate")
	err = os.MkdirAll(nmstateDir, os.ModePerm)
	if err != nil {
		return nil, err
	}
	// Remove temp dir
	defer func() {
		removeErr := os.RemoveAll(nmstateDir)
		if removeErr != nil {
			log.WithError(removeErr).Error("failed to remove nmstate temp dir")
		}
	}()

	binaryPath, err := n.extractNmstatectl(rootfsPath, nmstateDir)
	if err != nil {
		return nil, err
	}
	nmstatectlPath := filepath.Join(nmstateDir, binaryPath)

	// Check if nmstatectl binary file exists
	if _, err = os.Stat(nmstatectlPath); os.IsNotExist(err) {
		return nil, err
	}

	// Read binary
	nmstateBinContent, err := os.ReadFile(nmstatectlPath)
	if err != nil {
		return nil, err
	}

	// Create a compressed RAM disk image with the nmstatectl binary
	compressedCpio, err := generateCompressedCPIO([]fileEntry{
		{
			Content: nmstateBinContent,
			Path:    NmstatectlPathInRamdisk,
			Mode:    0o100_755,
		},
	})
	if err != nil {
		return nil, err
	}

	return compressedCpio, err
}

func (n *nmstateHandler) extractNmstatectl(rootfsPath, nmstateDir string) (string, error) {
	_, err := n.executer.Execute(fmt.Sprintf("cat %s | cpio -i", rootfsPath), nmstateDir)
	if err != nil {
		log.Errorf("failed to extract rootfs.img using cpio command: %v", err.Error())
		return "", err
	}

	nmstatectlExtractor, err := n.nmstatectlExtractorFactory.CreateNmstatectlExtractor(nmstateDir)
	if err != nil {
		log.Errorf("failed to create nmstate extractor: %v", err.Error())
		return "", err
	}

	return nmstatectlExtractor.ExtractNmstatectl(nmstateDir)
}

//go:generate mockgen -package=isoeditor -destination=mock_executer.go . Executer
type Executer interface {
	Execute(command, workDir string) (string, error)
}

type CommonExecuter struct{}

func (e *CommonExecuter) Execute(command, workDir string) (string, error) {
	var stdoutBytes, stderrBytes bytes.Buffer
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = &stdoutBytes
	cmd.Stderr = &stderrBytes
	log.Infof("Running cmd: %s", command)
	cmd.Dir = workDir
	err := cmd.Run()
	if err != nil {
		return "", errors.Wrapf(err, "Failed to execute cmd (%s): %s", cmd, stderrBytes.String())
	}

	return strings.TrimSuffix(stdoutBytes.String(), "\n"), nil
}

//go:generate mockgen -package=isoeditor -destination=mock_nmstatectl_extractor.go . NmstatectlExtractor
type NmstatectlExtractor interface {
	ExtractNmstatectl(nmstateDir string) (string, error)
}

//go:generate mockgen -package=isoeditor -destination=mock_nmstatectl_extractor_factory.go . NmstatectlExtractorFactory
type NmstatectlExtractorFactory interface {
	CreateNmstatectlExtractor(nmstateDir string) (NmstatectlExtractor, error)
}

type nmstatectlExtractorFactory struct {
	executer Executer
}

func NewNmstatectlExtractorFactory(executer Executer) NmstatectlExtractorFactory {
	return &nmstatectlExtractorFactory{
		executer: executer,
	}
}

func (f *nmstatectlExtractorFactory) CreateNmstatectlExtractor(nmstateDir string) (NmstatectlExtractor, error) {
	r, err := regexp.Compile(`root\.([^.]+)$`)
	if err != nil {
		log.Errorf("failed to compile regexp: %v", err.Error())
		return nil, err
	}

	entries, err := os.ReadDir(nmstateDir)
	if err != nil {
		log.Errorf("failed to list files in nmstateDir: %v", err.Error())
		return nil, err
	}

	extension := ""
	for _, entry := range entries {
		matches := r.FindStringSubmatch(entry.Name())
		if len(matches) > 1 {
			extension = matches[1]
		}
	}

	switch extension {
	case "squashfs":
		return &squashfsExtractor{executer: f.executer}, nil
	case "erofs":
		return &erofsExtractor{}, nil
	case "":
		return nil, errors.New("failed to find root file extension")
	default:
		return nil, fmt.Errorf("unknown extension for root file: %s", extension)
	}
}

type squashfsExtractor struct {
	executer Executer
}

// TODO: Update the code to utilize go-diskfs's squashfs instead of unsquashfs once go-diskfs supports the zstd compression format used by CoreOS - MGMT-19227
func (e *squashfsExtractor) ExtractNmstatectl(nmstateDir string) (string, error) {
	// limiting files is needed on el<=9 due to https://github.com/plougher/squashfs-tools/issues/125
	ulimit := "ulimit -n 1024"

	// Listing the filesystem concisely, displaying only files (using `-lc` option).
	// Each file in the output won't include any prefix before `/ostree` (by using `-dest ''` option),
	// which is useful when invoking `-extract-file` (after finding the `nmstatectl` binary path).
	list, err := e.executer.Execute(fmt.Sprintf("%s ; unsquashfs -dest '' -lc %s", ulimit, "root.squashfs"), nmstateDir)
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

	_, err = e.executer.Execute(fmt.Sprintf("%s ; unsquashfs -no-xattrs %s -extract-file %s", ulimit, "root.squashfs", binaryPath), nmstateDir)
	if err != nil {
		log.Errorf("failed to unsquashfs root.squashfs: %v", err.Error())
		return "", err
	}
	return filepath.Join("squashfs-root", binaryPath), nil
}

type erofsExtractor struct {
}

func (e *erofsExtractor) ExtractNmstatectl(nmstateDir string) (string, error) {
	rootErofsPath := filepath.Join(nmstateDir, "root.erofs")
	rootErofsFile, err := os.Open(rootErofsPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to open root.erofs")
	}
	defer rootErofsFile.Close()

	rootErofsInfo, err := rootErofsFile.Stat()
	if err != nil {
		return "", errors.Wrap(err, "failed to stat root.erofs")
	}

	rootErofsStorage := file.New(rootErofsFile, true)
	rootErofs, err := erofs.Read(rootErofsStorage, rootErofsInfo.Size(), 0, 0)
	if err != nil {
		log.Errorf("failed to read root.erofs: %v", err.Error())
		return "", err
	}
	defer rootErofs.Close()

	nmstatectlPath, err := findFileInErofs(rootErofs, "nmstatectl")
	if err != nil {
		log.Errorf("failed to search for nmstatectl in root.erofs: %v", err.Error())
		return "", err
	}

	nmstatectlInfo, err := rootErofs.Stat(nmstatectlPath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to stat %s in root.erofs", nmstatectlPath)
	}
	if !nmstatectlInfo.Mode().IsRegular() {
		return "", errors.Errorf("%s in root.erofs is not a regular file", nmstatectlPath)
	}

	nmstatectlContent, err := rootErofs.ReadFile(nmstatectlPath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read %s from root.erofs", nmstatectlPath)
	}

	nmstatectlFile, err := os.OpenFile(filepath.Join(nmstateDir, "nmstatectl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", errors.Wrap(err, "failed to create nmstatectl")
	}
	defer nmstatectlFile.Close()

	if _, err := nmstatectlFile.Write(nmstatectlContent); err != nil {
		return "", errors.Wrap(err, "failed to write nmstatectl")
	}
	if err := nmstatectlFile.Chmod(nmstatectlInfo.Mode().Perm()); err != nil {
		return "", errors.Wrap(err, "failed to set nmstatectl permissions")
	}

	return "nmstatectl", nil
}

func findFileInErofs(rootErofs *erofs.FileSystem, filename string) (string, error) {
	var found string
	err := fs.WalkDir(rootErofs, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if found != "" {
			return nil
		}
		if entry.Name() == filename && entry.Type().IsRegular() {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.Errorf("%s not found in root.erofs", filename)
	}
	return found, nil
}

package isoeditor

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cavaliercoder/go-cpio"
	"github.com/diskfs/go-diskfs/backend"
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
	nmstatectlExtractor, err := n.nmstatectlExtractorFactory.CreateNmstatectlExtractor(rootfsPath)
	if err != nil {
		log.Errorf("failed to create nmstate extractor: %v", err.Error())
		return "", err
	}

	return nmstatectlExtractor.ExtractNmstatectl(rootfsPath, nmstateDir)
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
	ExtractNmstatectl(rootfsPath, nmstateDir string) (string, error)
}

//go:generate mockgen -package=isoeditor -destination=mock_nmstatectl_extractor_factory.go . NmstatectlExtractorFactory
type NmstatectlExtractorFactory interface {
	CreateNmstatectlExtractor(rootfsPath string) (NmstatectlExtractor, error)
}

type nmstatectlExtractorFactory struct {
	executer Executer
}

func NewNmstatectlExtractorFactory(executer Executer) NmstatectlExtractorFactory {
	return &nmstatectlExtractorFactory{
		executer: executer,
	}
}

func (f *nmstatectlExtractorFactory) CreateNmstatectlExtractor(rootfsPath string) (NmstatectlExtractor, error) {
	extension, erofsOffset, erofsSize, err := inspectRootfs(rootfsPath)
	if err != nil {
		return nil, err
	}

	switch extension {
	case "squashfs":
		return &squashfsExtractor{executer: f.executer}, nil
	case "erofs":
		return &erofsExtractor{offset: erofsOffset, size: erofsSize}, nil
	default:
		return nil, fmt.Errorf("unknown extension for root file: %s", extension)
	}
}

func inspectRootfs(rootfsPath string) (extension string, erofsOffset, erofsSize int64, err error) {
	rootfsFile, err := os.Open(rootfsPath)
	if err != nil {
		return "", 0, 0, errors.Wrap(err, "failed to open rootfs.img")
	}
	defer rootfsFile.Close()

	rootfsInfo, err := rootfsFile.Stat()
	if err != nil {
		return "", 0, 0, errors.Wrap(err, "failed to stat rootfs.img")
	}

	reader := io.NewSectionReader(rootfsFile, 0, rootfsInfo.Size())
	archive := cpio.NewReader(reader)
	rootFileRegex := regexp.MustCompile(`^root\.([^.]+)$`)

	for {
		header, headerErr := archive.Next()
		if errors.Is(headerErr, io.EOF) {
			return "", 0, 0, errors.New("failed to find root file in rootfs.img")
		}
		if headerErr != nil {
			return "", 0, 0, errors.Wrap(headerErr, "failed to read rootfs.img CPIO header")
		}

		matches := rootFileRegex.FindStringSubmatch(header.Name)
		if len(matches) != 2 {
			continue
		}
		if !header.Mode.IsRegular() {
			return "", 0, 0, errors.Errorf("root file %s in rootfs.img is not a regular file", header.Name)
		}

		if matches[1] != "erofs" {
			return matches[1], 0, 0, nil
		}

		erofsOffset, headerErr = reader.Seek(0, io.SeekCurrent)
		if headerErr != nil {
			return "", 0, 0, errors.Wrap(headerErr, "failed to locate root.erofs data in rootfs.img")
		}
		return "erofs", erofsOffset, header.Size, nil
	}
}

type squashfsExtractor struct {
	executer Executer
}

// TODO: Update the code to utilize go-diskfs's squashfs instead of unsquashfs once go-diskfs supports the zstd compression format used by CoreOS - MGMT-19227
func (e *squashfsExtractor) ExtractNmstatectl(rootfsPath, nmstateDir string) (string, error) {
	_, err := e.executer.Execute(fmt.Sprintf("cat %s | cpio -i", rootfsPath), nmstateDir)
	if err != nil {
		log.Errorf("failed to extract rootfs.img using cpio command: %v", err.Error())
		return "", err
	}

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
	offset int64
	size   int64
}

func (e *erofsExtractor) ExtractNmstatectl(rootfsPath, nmstateDir string) (string, error) {
	rootfsFile, err := os.Open(rootfsPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to open rootfs.img")
	}
	defer rootfsFile.Close()

	rootfsStorage := file.New(rootfsFile, true)
	rootErofsStorage := backend.Sub(rootfsStorage, e.offset, e.size)
	rootErofs, err := erofs.Read(rootErofsStorage, e.size, 0, 0)
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

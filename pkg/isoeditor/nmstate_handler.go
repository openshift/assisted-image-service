package isoeditor

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/cavaliercoder/go-cpio"
	"github.com/diskfs/go-diskfs/backend"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/erofs"
	"github.com/diskfs/go-diskfs/filesystem/squashfs"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

//go:generate mockgen -package=isoeditor -destination=mock_nmstate_handler.go . NmstateHandler
type NmstateHandler interface {
	BuildNmstateCpioArchive(rootfsPath string) ([]byte, error)
}

type nmstateHandler struct {
	workDir                    string
	nmstatectlExtractorFactory NmstatectlExtractorFactory
}

func NewNmstateHandler(workDir string, nmstatectlExtractorFactory NmstatectlExtractorFactory) NmstateHandler {
	return &nmstateHandler{
		workDir:                    workDir,
		nmstatectlExtractorFactory: nmstatectlExtractorFactory,
	}
}

func (n *nmstateHandler) BuildNmstateCpioArchive(rootfsPath string) ([]byte, error) {
	content, err := n.extractNmstatectl(rootfsPath)
	if err != nil {
		return nil, err
	}

	return generateCompressedCPIO([]fileEntry{
		{
			Content: content,
			Path:    NmstatectlPathInRamdisk,
			Mode:    0o100_755,
		},
	})
}

func (n *nmstateHandler) extractNmstatectl(rootfsPath string) ([]byte, error) {
	nmstatectlExtractor, err := n.nmstatectlExtractorFactory.CreateNmstatectlExtractor(rootfsPath)
	if err != nil {
		log.Errorf("failed to create nmstate extractor: %v", err.Error())
		return nil, err
	}

	return nmstatectlExtractor.ExtractNmstatectl(rootfsPath, n.workDir)
}

//go:generate mockgen -package=isoeditor -destination=mock_nmstatectl_extractor.go . NmstatectlExtractor
type NmstatectlExtractor interface {
	ExtractNmstatectl(rootfsPath, workDir string) ([]byte, error)
}

//go:generate mockgen -package=isoeditor -destination=mock_nmstatectl_extractor_factory.go . NmstatectlExtractorFactory
type NmstatectlExtractorFactory interface {
	CreateNmstatectlExtractor(rootfsPath string) (NmstatectlExtractor, error)
}

type nmstatectlExtractorFactory struct {
}

func NewNmstatectlExtractorFactory() NmstatectlExtractorFactory {
	return &nmstatectlExtractorFactory{}
}

func (f *nmstatectlExtractorFactory) CreateNmstatectlExtractor(rootfsPath string) (NmstatectlExtractor, error) {
	extension, rootOffset, rootSize, err := inspectRootfs(rootfsPath)
	if err != nil {
		return nil, err
	}

	switch extension {
	case "squashfs":
		return &squashfsExtractor{offset: rootOffset, size: rootSize}, nil
	case "erofs":
		return &erofsExtractor{offset: rootOffset, size: rootSize}, nil
	default:
		return nil, fmt.Errorf("unknown extension for root file: %s", extension)
	}
}

func inspectRootfs(rootfsPath string) (extension string, rootOffset, rootSize int64, err error) {
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

		matches := rootFileRegex.FindStringSubmatch(strings.TrimPrefix(header.Name, "./"))
		if len(matches) != 2 {
			continue
		}
		if !header.Mode.IsRegular() {
			return "", 0, 0, errors.Errorf("root file %s in rootfs.img is not a regular file", header.Name)
		}

		rootOffset, headerErr = reader.Seek(0, io.SeekCurrent)
		if headerErr != nil {
			return "", 0, 0, errors.Wrap(headerErr, "failed to locate root filesystem data in rootfs.img")
		}
		if header.Size <= 0 {
			return "", 0, 0, errors.Errorf("root file %s in rootfs.img has invalid size %d", header.Name, header.Size)
		}
		if rootOffset > rootfsInfo.Size() || header.Size > rootfsInfo.Size()-rootOffset {
			return "", 0, 0, errors.Errorf("root file %s extends beyond rootfs.img", header.Name)
		}
		return matches[1], rootOffset, header.Size, nil
	}
}

type squashfsExtractor struct {
	offset int64
	size   int64
}

func (e *squashfsExtractor) ExtractNmstatectl(rootfsPath, _ string) ([]byte, error) {
	rootfsFile, err := os.Open(rootfsPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open rootfs.img")
	}
	defer rootfsFile.Close()

	rootfsStorage := file.New(rootfsFile, true)
	rootStorage := backend.Sub(rootfsStorage, e.offset, e.size)
	rootFS, err := squashfs.Read(rootStorage, e.size, 0, 0)
	if err != nil {
		log.Errorf("failed to read root.squashfs: %v", err.Error())
		return nil, err
	}
	defer rootFS.Close()

	return extractNmstatectl(rootFS, "root.squashfs")
}

type erofsExtractor struct {
	offset int64
	size   int64
}

func (e *erofsExtractor) ExtractNmstatectl(rootfsPath, _ string) ([]byte, error) {
	rootfsFile, err := os.Open(rootfsPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open rootfs.img")
	}
	defer rootfsFile.Close()

	rootfsStorage := file.New(rootfsFile, true)
	rootStorage := backend.Sub(rootfsStorage, e.offset, e.size)
	rootFS, err := erofs.Read(rootStorage, e.size, 0, 0)
	if err != nil {
		log.Errorf("failed to read root.erofs: %v", err.Error())
		return nil, err
	}
	defer rootFS.Close()

	return extractNmstatectl(rootFS, "root.erofs")
}

type rootFilesystem interface {
	fs.FS
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
}

func extractNmstatectl(rootFS rootFilesystem, rootName string) ([]byte, error) {
	nmstatectlPath, err := findFileInRootfs(rootFS, "nmstatectl", rootName)
	if err != nil {
		log.Errorf("failed to search for nmstatectl in %s: %v", rootName, err.Error())
		return nil, err
	}

	nmstatectlInfo, err := rootFS.Stat(nmstatectlPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat %s in %s", nmstatectlPath, rootName)
	}
	if !nmstatectlInfo.Mode().IsRegular() {
		return nil, errors.Errorf("%s in %s is not a regular file", nmstatectlPath, rootName)
	}

	nmstatectlContent, err := rootFS.ReadFile(nmstatectlPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s from %s", nmstatectlPath, rootName)
	}
	if int64(len(nmstatectlContent)) != nmstatectlInfo.Size() {
		return nil, errors.Errorf("incomplete read of %s from %s: got %d bytes, expected %d", nmstatectlPath, rootName, len(nmstatectlContent), nmstatectlInfo.Size())
	}

	return nmstatectlContent, nil
}

func findFileInRootfs(rootFS fs.FS, filename, rootName string) (string, error) {
	var found string
	err := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
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
		return "", errors.Errorf("%s not found in %s", filename, rootName)
	}
	return found, nil
}

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
}

func NewNmstateHandler() NmstateHandler {
	return &nmstateHandler{}
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
	root, err := inspectRootfs(rootfsPath)
	if err != nil {
		log.Errorf("failed to inspect rootfs.img: %v", err.Error())
		return nil, err
	}

	rootfsFile, err := os.Open(rootfsPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open rootfs.img")
	}
	defer rootfsFile.Close()

	rootStorage := backend.Sub(file.New(rootfsFile, true), root.Offset, root.Size)
	var rootFS rootFilesystem
	switch root.Extension {
	case "squashfs":
		rootFS, err = squashfs.Read(rootStorage, root.Size, 0, 0)
	case "erofs":
		rootFS, err = erofs.Read(rootStorage, root.Size, 0, 0)
	default:
		return nil, fmt.Errorf("unknown extension for root file: %s", root.Name)
	}
	if err != nil {
		log.Errorf("failed to read %s: %v", root.Name, err.Error())
		return nil, err
	}
	defer rootFS.Close()

	return extractNmstatectl(rootFS, root.Name)
}

type rootfsMember struct {
	Name      string
	Extension string
	Offset    int64
	Size      int64
}

func inspectRootfs(rootfsPath string) (rootfsMember, error) {
	rootfsFile, err := os.Open(rootfsPath)
	if err != nil {
		return rootfsMember{}, errors.Wrap(err, "failed to open rootfs.img")
	}
	defer rootfsFile.Close()

	rootfsInfo, err := rootfsFile.Stat()
	if err != nil {
		return rootfsMember{}, errors.Wrap(err, "failed to stat rootfs.img")
	}

	reader := io.NewSectionReader(rootfsFile, 0, rootfsInfo.Size())
	archive := cpio.NewReader(reader)
	rootFileRegex := regexp.MustCompile(`^root\.([^.]+)$`)

	for {
		header, headerErr := archive.Next()
		if errors.Is(headerErr, io.EOF) {
			return rootfsMember{}, errors.New("failed to find root file in rootfs.img")
		}
		if headerErr != nil {
			return rootfsMember{}, errors.Wrap(headerErr, "failed to read rootfs.img CPIO header")
		}

		matches := rootFileRegex.FindStringSubmatch(strings.TrimPrefix(header.Name, "./"))
		if len(matches) != 2 {
			continue
		}
		if !header.Mode.IsRegular() {
			return rootfsMember{}, errors.Errorf("root file %s in rootfs.img is not a regular file", header.Name)
		}

		rootOffset, headerErr := reader.Seek(0, io.SeekCurrent)
		if headerErr != nil {
			return rootfsMember{}, errors.Wrap(headerErr, "failed to locate root filesystem data in rootfs.img")
		}
		if header.Size <= 0 {
			return rootfsMember{}, errors.Errorf("root file %s in rootfs.img has invalid size %d", header.Name, header.Size)
		}
		if rootOffset > rootfsInfo.Size() || header.Size > rootfsInfo.Size()-rootOffset {
			return rootfsMember{}, errors.Errorf("root file %s extends beyond rootfs.img", header.Name)
		}
		return rootfsMember{
			Name:      header.Name,
			Extension: matches[1],
			Offset:    rootOffset,
			Size:      header.Size,
		}, nil
	}
}

type rootFilesystem interface {
	fs.FS
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	Close() error
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

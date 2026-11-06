package isoeditor

import (
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"

	"github.com/cavaliercoder/go-cpio"
	"github.com/diskfs/go-diskfs/backend"
	"github.com/diskfs/go-diskfs/filesystem/erofs"
	"github.com/diskfs/go-diskfs/filesystem/squashfs"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

//go:generate mockgen -package=isoeditor -destination=mock_nmstate_handler.go . NmstateHandler
type NmstateHandler interface {
	BuildNmstateCpioArchiveFromISO(isoPath, rootfsPath string, output io.Writer) error
}

type nmstateHandler struct {
}

func NewNmstateHandler() NmstateHandler {
	return &nmstateHandler{}
}

func (n *nmstateHandler) BuildNmstateCpioArchiveFromISO(isoPath, rootfsPath string, output io.Writer) error {
	rootfsStorage, rootfsSize, err := GetISOFileStorage(isoPath, rootfsPath)
	if err != nil {
		return err
	}
	defer rootfsStorage.Close()

	return n.writeNmstatectlCpioArchive(rootfsStorage, rootfsSize, output)
}

func (n *nmstateHandler) writeNmstatectlCpioArchive(rootfsStorage backend.Storage, rootfsSize int64, output io.Writer) error {
	root, err := inspectRootfs(rootfsStorage, rootfsSize)
	if err != nil {
		log.Errorf("failed to inspect rootfs.img: %v", err.Error())
		return err
	}

	rootStorage := backend.Sub(rootfsStorage, root.Offset, root.Size)
	var rootFS rootFilesystem
	switch root.Extension {
	case "squashfs":
		rootFS, err = squashfs.Read(rootStorage, root.Size, 0, 0)
	case "erofs":
		rootFS, err = erofs.Read(rootStorage, root.Size, 0, 0)
	default:
		return fmt.Errorf("unknown extension for root file: %s", root.Name)
	}
	if err != nil {
		log.Errorf("failed to read %s: %v", root.Name, err.Error())
		return err
	}
	defer rootFS.Close()

	nmstatectlPath, err := findFileInRootfs(rootFS, "nmstatectl", root.Name)
	if err != nil {
		log.Errorf("failed to search for nmstatectl in %s: %v", root.Name, err.Error())
		return err
	}

	nmstatectlInfo, err := rootFS.Stat(nmstatectlPath)
	if err != nil {
		return errors.Wrapf(err, "failed to stat %s in %s", nmstatectlPath, root.Name)
	}
	if !nmstatectlInfo.Mode().IsRegular() {
		return errors.Errorf("%s in %s is not a regular file", nmstatectlPath, root.Name)
	}

	nmstatectlFile, err := rootFS.Open(nmstatectlPath)
	if err != nil {
		return errors.Wrapf(err, "failed to open %s from %s", nmstatectlPath, root.Name)
	}
	defer nmstatectlFile.Close()

	return writeCompressedCPIO(output, []fileEntry{
		{
			Reader: nmstatectlFile,
			Path:   NmstatectlPathInRamdisk,
			Mode:   0o100_755,
			Size:   nmstatectlInfo.Size(),
		},
	})
}

type rootfsMember struct {
	Name      string
	Extension string
	Offset    int64
	Size      int64
}

func inspectRootfs(rootfsStorage backend.Storage, rootfsSize int64) (rootfsMember, error) {
	reader := io.NewSectionReader(rootfsStorage, 0, rootfsSize)
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
		if rootOffset > rootfsSize || header.Size > rootfsSize-rootOffset {
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
	Close() error
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

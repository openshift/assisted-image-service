package isoeditor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/openshift/assisted-image-service/pkg/overlay"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	ignitionInfoPath         = "/coreos/igninfo.json"
	defaultIgnitionImagePath = "/images/ignition.img"
)

type ImageReader = overlay.OverlayReader

type BoundariesFinder func(filePath, isoPath string) (int64, int64, error)

type StreamGeneratorFunc func(isoPath string, ignitionContent *IgnitionContent, ramdiskContent, kargs []byte) (ImageReader, error)

// IgnInfo us used to read and write the content of the '/coreos/igninfo.json' file that indicates
// the location of the ignition configuration inside the ISO.
type IgnInfo struct {
	// File is the absolute path of the file containing the ignition configuration.
	File string `json:"file"`

	// Offset is the offset of the ignition configuration file within the file.
	Offset int64 `json:"offset,omitempty"`

	// Length is the length of the ignition configuration.
	Length int64 `json:"length,omitempty"`
}

func NewRHCOSStreamReader(isoPath string, ignitionContent *IgnitionContent, ramdiskContent []byte, kargs []byte) (ImageReader, error) {
	isoReader, err := os.Open(isoPath)
	if err != nil {
		return nil, err
	}

	ignitionReader, err := ignitionContent.Archive()
	if err != nil {
		return nil, err
	}

	// Starting with version 0.17.0 of the CoreOS installer a new '/coreos/igninfo.json' file may
	// exist to indicate the location of the ignition inside the ISO. For example, in the S390
	// platform it will contain something like this:
	//
	//	{
	//		"file": "images/cdboot.img",
	//		"offset": 66497660,
	//		"length": 262144
	//	}
	//
	// If it exists we need to read it, otherwise we can use the '/images/ignition.img' location
	// used in previous versions.
	var ignInfo IgnInfo
	ignInfoBytes, err := ReadFileFromISO(isoPath, ignitionInfoPath)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"iso":     isoPath,
			"file":    ignitionInfoPath,
			"default": defaultIgnitionImagePath,
		}).Info(
			"Failed to read ignition information file, will assume that it doesn't exist " +
				"and use the default ignition location",
		)
		ignInfo.File = defaultIgnitionImagePath
		ignInfo.Offset = 0
		_, ignInfo.Length, err = GetISOFileInfo(defaultIgnitionImagePath, isoPath)
		if err != nil {
			return nil, err
		}
	} else {
		err = json.Unmarshal(ignInfoBytes, &ignInfo)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				"failed to unmarshal ignition information from file '%s' of ISO '%s'",
				ignitionInfoPath, isoPath,
			)
		}
	}

	// We know now the offset and length of the ignition inside the file that contains it, but
	// we need to calculate the offset inside the ISO, and for that we need the offset and
	// length of that container.
	containerOffset, containerLength, err := GetISOFileInfo(ignInfo.File, isoPath)
	if err != nil {
		return nil, err
	}

	// When the length of the ignition is not explicitly specified it is the total length of the
	// container file:
	if ignInfo.Length == 0 {
		ignInfo.Length = containerLength
	}

	// Create a boundaries finder that calculates the location of the ignition relative to the
	// complete ISO file:
	ignBoundariesFinder := func(filePath, isoPath string) (ignOffset int64, ignLength int64, err error) {
		ignOffset = containerOffset + ignInfo.Offset
		ignLength = ignInfo.Length
		return
	}

	r, err := readerForContent(isoPath, ignInfo.File, isoReader, ignitionReader, ignBoundariesFinder)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create overwrite reader for ignition")
	}

	if ramdiskContent != nil {
		r, err = readerForFileContent(isoPath, ramDiskImagePath, r, bytes.NewReader(ramdiskContent))
		if err != nil {
			return nil, errors.Wrap(err, "failed to create overwrite reader for ramdisk")
		}
	}

	if kargs != nil {
		files, err := KargsFiles(isoPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read files to patch for kernel arguments")
		}
		for _, file := range files {
			r, err = readerForKargsContent(isoPath, file, r, bytes.NewReader(kargs))
			if err != nil {
				return nil, errors.Wrapf(err, "failed to create overwrite reader for kernel arguments in file \"%s\"", file)
			}
		}
	}

	return r, nil
}

func readerForContent(isoPath, filePath string, base io.ReadSeeker, contentReader *bytes.Reader, boundariesFinder BoundariesFinder) (overlay.OverlayReader, error) {
	start, length, err := boundariesFinder(filePath, isoPath)
	if err != nil {
		return nil, err
	}

	if length < contentReader.Size() {
		return nil, errors.New(fmt.Sprintf("content length (%d) exceeds embed area size (%d)", contentReader.Size(), length))
	}

	rdOverlay := overlay.Overlay{
		Reader: contentReader,
		Offset: start,
		Length: contentReader.Size(),
	}
	r, err := overlay.NewOverlayReader(base, rdOverlay)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func readerForFileContent(isoPath string, filePath string, base io.ReadSeeker, contentReader *bytes.Reader) (overlay.OverlayReader, error) {
	return readerForContent(isoPath, filePath, base, contentReader, GetISOFileInfo)
}

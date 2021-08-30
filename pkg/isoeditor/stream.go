package isoeditor

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/openshift/assisted-image-service/pkg/overlay"
	"github.com/pkg/errors"
)

const ignitionImagePath = "/images/ignition.img"

type StreamGeneratorFunc func(isoPath string, ignitionContent []byte, ramdiskContent []byte) (io.ReadSeeker, error)

func NewRHCOSStreamReader(isoPath string, ignitionContent []byte, ramdiskContent []byte) (io.ReadSeeker, error) {
	isoReader, err := os.Open(isoPath)
	if err != nil {
		return nil, err
	}

	r, err := readerForContent(isoPath, ignitionImagePath, isoReader, ignitionContent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create overwrite reader for ignition")
	}

	if ramdiskContent != nil {
		r, err = readerForContent(isoPath, ramDiskImagePath, r, ramdiskContent)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create overwrite reader for ramdisk")
		}
	}

	return r, nil
}

func readerForContent(isoPath string, filePath string, base io.ReadSeeker, content []byte) (io.ReadSeeker, error) {
	start, length, err := GetISOFileInfo(filePath, isoPath)
	if err != nil {
		return nil, err
	}

	contentReader := bytes.NewReader(content)
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

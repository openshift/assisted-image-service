package isoeditor

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openshift/assisted-image-service/pkg/overlay"
	"github.com/pkg/errors"
)

const ignitionImagePath = "/images/ignition.img"

type StreamGeneratorFunc func(isoPath string, ignitionContent string) (io.ReadSeeker, error)

func NewRHCOSStreamReader(isoPath string, ignitionContent string) (io.ReadSeeker, error) {
	areaStart, areaLength, err := GetISOFileInfo(ignitionImagePath, isoPath)
	if err != nil {
		return nil, err
	}

	ignitionReader := strings.NewReader(ignitionContent)
	if areaLength < ignitionReader.Size() {
		return nil, errors.New(fmt.Sprintf("ignition length (%d) exceeds embed area size (%d)", ignitionReader.Size(), areaLength))
	}

	isoReader, err := os.Open(isoPath)
	if err != nil {
		return nil, err
	}

	ignitionOverlay := overlay.Overlay{
		Reader: ignitionReader,
		Offset: areaStart,
		Length: ignitionReader.Size(),
	}
	contentReader, err := overlay.NewOverlayReader(isoReader, ignitionOverlay)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create overwrite reader")
	}

	return contentReader, nil
}

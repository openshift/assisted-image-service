package isoeditor

import "io"

type FileData struct {
	Filename string
	Data     io.ReadCloser
}

// NewIgnitionImageReader returns the filename of the ignition image in the ISO,
// along with a stream of the ignition image with ignition content embedded.
// This can be used to overwrite the ignition image file of an ISO previously
// unpacked by Extract() in order to embed ignition data.
func NewIgnitionImageReader(isoPath string, ignitionContent *IgnitionContent) (FileData, error) {
	info, iso, err := ignitionOverlay(isoPath, ignitionContent)
	if err != nil {
		return FileData{}, err
	}
	imageOffset, imageLength, err := GetISOFileInfo(info.File, isoPath)
	if err != nil {
		return FileData{}, err
	}

	if _, err := iso.Seek(imageOffset, io.SeekStart); err != nil {
		iso.Close()
		return FileData{}, err
	}
	data := struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(iso, imageLength),
		Closer: iso,
	}

	ignitionImage := FileData{
		Filename: info.File,
		Data:     data,
	}
	return ignitionImage, nil
}

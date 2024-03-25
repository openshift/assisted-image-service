package isoeditor

import (
	"bytes"
	"encoding/json"
	"io"
)

type FileData struct {
	Filename string
	Data     io.ReadCloser
}

// NewIgnitionImageReader returns the filename of the ignition image in the ISO,
// along with a stream of the ignition image with ignition content embedded.
// This can be used to overwrite the ignition image file of an ISO previously
// unpacked by Extract() in order to embed ignition data.
func NewIgnitionImageReader(isoPath string, ignitionContent *IgnitionContent) ([]FileData, error) {
	info, iso, err := ignitionOverlay(isoPath, ignitionContent, true)
	if err != nil {
		return nil, err
	}

	imageOffset, imageLength, err := GetISOFileInfo(info.File, isoPath)
	if err != nil {
		return nil, err
	}

	length := info.Offset + info.Length
	// include any trailing data
	if imageLength > length {
		length = imageLength
	}

	if _, err := iso.Seek(imageOffset, io.SeekStart); err != nil {
		iso.Close()
		return nil, err
	}
	data := struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(iso, length),
		Closer: iso,
	}

	output := []FileData{{
		Filename: info.File,
		Data:     data,
	}}

	// output updated igninfo.json if we have expanded the embed area
	if length > imageLength {
		if _, _, err := GetISOFileInfo(ignitionInfoPath, isoPath); err == nil {
			if ignitionInfoData, err := json.Marshal(info); err == nil {
				output = append(output, FileData{
					Filename: ignitionInfoPath,
					Data:     io.NopCloser(bytes.NewReader(ignitionInfoData)),
				})
			} else {
				iso.Close()
				return nil, err
			}
		}
	}

	return output, nil
}

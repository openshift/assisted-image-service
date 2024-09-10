package isoeditor

import (
	"bytes"
)

type IgnitionContent struct {
	Config []byte
}

func (ic *IgnitionContent) Archive() (*bytes.Reader, error) {
	compressedCpio, err := generateCompressedCPIO(ic.Config, "config.ign", 0o100_644)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(compressedCpio), nil
}

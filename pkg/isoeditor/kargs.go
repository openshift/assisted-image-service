package isoeditor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/openshift/assisted-image-service/pkg/overlay"
)

const (
	defaultGrubFilePath     = "/EFI/redhat/grub.cfg"
	defaultIsolinuxFilePath = "/isolinux/isolinux.cfg"
	kargsConfigFilePath     = "/coreos/kargs.json"
)

type FileReader func(isoPath, filePath string) ([]byte, error)

func kargsFiles(isoPath string, fileReader FileReader) ([]string, error) {
	kargsData, err := fileReader(isoPath, kargsConfigFilePath)
	if err != nil {
		// If the kargs file is not found, it is probably iso for old iso version which the file does not exist.  Therefore,
		// default is returned
		return []string{defaultGrubFilePath, defaultIsolinuxFilePath}, nil
	}
	var kargsConfig struct {
		Files []struct {
			Path *string
		}
	}
	if err := json.Unmarshal(kargsData, &kargsConfig); err != nil {
		return nil, err
	}
	var ret []string
	for _, file := range kargsConfig.Files {
		if file.Path != nil {
			ret = append(ret, *file.Path)
		}
	}
	return ret, nil
}

func KargsFiles(isoPath string) ([]string, error) {
	return kargsFiles(isoPath, ReadFileFromISO)
}

// EmbedKargsIntoBootImage appends custom kernel arguments into a staging ISO image that
// already contains an ignition config, using offsets and size limits defined in `coreos/kargs.json`
// that are extracted from the original base ISO.
//
// This function is only invoked when both the ignition config and kernel arguments must be embedded
// into the same boot image.
func EmbedKargsIntoBootImage(baseIsoPath string, stagingIsoPath string, customKargs string) error {

	// Read the kargs.json file content from the ISO
	kargsData, err := ReadFileFromISO(baseIsoPath, kargsConfigFilePath)
	if err != nil {
		return fmt.Errorf("failed to read kargs config from %s: %w", kargsConfigFilePath, err)
	}

	// Loading the kargs config JSON file
	var kargsConfig struct {
		Default string `json:"default"`
		Files   []struct {
			Path   string `json:"path"`
			Offset int64  `json:"offset"`
			End    string `json:"end"`
			Pad    string `json:"pad"`
		} `json:"files"`
		Size int `json:"size"`
	}
	if err := json.Unmarshal(kargsData, &kargsConfig); err != nil {
		return fmt.Errorf("failed to parse %s: %w", kargsConfigFilePath, err)
	}

	// Make sure kargs config files are present
	if len(kargsConfig.Files) == 0 {
		return fmt.Errorf("no kargs file entries found in %s", kargsConfigFilePath)
	}

	// Fetch kargs files from the ISO
	files, err := KargsFiles(baseIsoPath)
	if err != nil {
		return err
	}

	// Embed kargs config into each file
	for _, filePath := range files {
		// Check if file exists
		absFilePath := filepath.Join(stagingIsoPath, filePath)
		fileExists, err := fileExists(absFilePath)
		if err != nil {
			return err
		}
		if !fileExists {
			return fmt.Errorf("file %s does not exist", absFilePath)
		}

		// Finding offset for the target filePath
		var kargsOffset int64
		for _, file := range kargsConfig.Files {
			if file.Path == filePath {
				kargsOffset = file.Offset
				break
			}
		}

		// Calculate the customKargsOffset
		existingKargs := []byte(kargsConfig.Default)
		appendKargsOffset := kargsOffset + int64(len(existingKargs))

		// Now open the file for read/write and patch at offset
		f, err := os.OpenFile(absFilePath, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("failed to open target file %s: %w", absFilePath, err)
		}
		defer f.Close()

		// Seek to the kargs offset in the filePath
		_, err = f.Seek(appendKargsOffset, io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek to kargs offset %d in %s: %w", appendKargsOffset, absFilePath, err)
		}

		// Determine available kargs field size if possible
		var maxLen int64
		if kargsConfig.Size > 0 {
			maxLen = int64(kargsConfig.Size)
		} else {
			// Try to get remaining bytes until next file or EOF (best-effort)
			// If we can't determine a safe max, at least ensure we don't write beyond file size.
			fi, statErr := f.Stat()
			if statErr == nil {
				maxLen = fi.Size() - appendKargsOffset
			}
		}

		// Ensure to not overflow the kargs field size
		kargsLength := len(existingKargs) + len(customKargs)
		if maxLen > 0 && int64(kargsLength) > maxLen {
			return fmt.Errorf("kargs length %d exceeds available field size %d", kargsLength, maxLen)
		}

		// Write the kargs bytes
		if _, err = f.Write([]byte(customKargs)); err != nil {
			return fmt.Errorf("failed writing kargs into %s: %w", absFilePath, err)
		}
	}

	return nil
}

func kargsFileData(isoPath string, file string, appendKargs []byte) (FileData, error) {
	baseISO, err := os.Open(isoPath)
	if err != nil {
		return FileData{}, err
	}

	iso, err := readerForKargsContent(isoPath, file, baseISO, bytes.NewReader(appendKargs))
	if err != nil {
		baseISO.Close()
		return FileData{}, err
	}

	fileData, _, err := isolateISOFile(isoPath, file, iso, 0)
	if err != nil {
		iso.Close()
		return FileData{}, err
	}

	return fileData, nil
}

// NewKargsReader returns the filename within an ISO and the new content of
// the file(s) containing the kernel arguments, with additional arguments
// appended.
func NewKargsReader(isoPath string, appendKargs string) ([]FileData, error) {
	if appendKargs == "" || appendKargs == "\n" {
		return nil, nil
	}
	appendData := []byte(appendKargs)
	if appendData[len(appendData)-1] != '\n' {
		appendData = append(appendData, '\n')
	}

	files, err := KargsFiles(isoPath)
	if err != nil {
		return nil, err
	}

	output := []FileData{}
	for i, f := range files {
		data, err := kargsFileData(isoPath, f, appendData)
		if err != nil {
			for _, fd := range output[:i] {
				fd.Data.Close()
			}
			return nil, err
		}

		output = append(output, data)
	}
	return output, nil
}

func kargsEmbedAreaBoundariesFinder(isoPath, filePath string, fileBoundariesFinder BoundariesFinder, fileReader FileReader) (int64, int64, error) {
	start, _, err := fileBoundariesFinder(filePath, isoPath)
	if err != nil {
		return 0, 0, err
	}

	b, err := fileReader(isoPath, filePath)
	if err != nil {
		return 0, 0, err
	}

	re := regexp.MustCompile(`(\n#*)# COREOS_KARG_EMBED_AREA`)
	submatchIndexes := re.FindSubmatchIndex(b)
	if len(submatchIndexes) != 4 {
		return 0, 0, errors.New("failed to find COREOS_KARG_EMBED_AREA")
	}
	return start + int64(submatchIndexes[2]), int64(submatchIndexes[3] - submatchIndexes[2]), nil
}

func createKargsEmbedAreaBoundariesFinder() BoundariesFinder {
	return func(filePath, isoPath string) (int64, int64, error) {
		return kargsEmbedAreaBoundariesFinder(isoPath, filePath, GetISOFileInfo, ReadFileFromISO)
	}
}

func readerForKargsContent(isoPath string, filePath string, base io.ReadSeeker, contentReader *bytes.Reader) (overlay.OverlayReader, error) {
	return readerForContent(isoPath, filePath, base, contentReader, createKargsEmbedAreaBoundariesFinder())
}

type kernelArgument struct {
	// The operation to apply on the kernel argument.
	// Enum: [append replace delete]
	Operation string `json:"operation,omitempty"`

	// Kernel argument can have the form <parameter> or <parameter>=<value>. The following examples should
	// be supported:
	// rd.net.timeout.carrier=60
	// isolcpus=1,2,10-20,100-2000:2/25
	// quiet
	// The parsing by the command line parser in linux kernel is much looser and this pattern follows it.
	Value string `json:"value,omitempty"`
}

type kernelArguments []*kernelArgument

func KargsToStr(args []string) (string, error) {
	var kargs kernelArguments
	for _, s := range args {
		kargs = append(kargs, &kernelArgument{
			Operation: "append",
			Value:     s,
		})
	}
	b, err := json.Marshal(&kargs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal kernel arguments %v", err)
	}
	return string(b), nil
}

func StrToKargs(kargsStr string) ([]string, error) {
	var kargs kernelArguments
	if err := json.Unmarshal([]byte(kargsStr), &kargs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal kernel arguments %v", err)
	}
	var args []string
	for _, arg := range kargs {
		if arg.Operation != "append" {
			return nil, fmt.Errorf("only 'append' operation is allowed.  got %s", arg.Operation)
		}
		args = append(args, arg.Value)
	}
	return args, nil
}

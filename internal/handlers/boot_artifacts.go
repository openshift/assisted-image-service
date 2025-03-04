package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
)

type BootArtifactsHandler struct {
	ImageStore imagestore.ImageStore
}

var _ http.Handler = &BootArtifactsHandler{}

var IsoFileName string

var bootpathRegexp = regexp.MustCompile(`^/boot-artifacts/(.+)`)

func parseArtifact(path, arch, version string) (string, error) {
	match := bootpathRegexp.FindStringSubmatch(path)
	if len(match) < 1 {
		return "", fmt.Errorf("malformed download path: %s", path)
	}

	var artifact string
	// Fetching rhelVersion from IsoFileName
	rhelVersion, err := strconv.Atoi(strings.Split(strings.Split(IsoFileName, version+"-")[1], ".")[1])
	if err != nil {
		fmt.Println("Error in fetching RHCOS Version from ISO file")
		return "", err
	}
	switch match[1] {
	case "rootfs":
		artifact = "rootfs.img"
	case "kernel":
		artifact = "vmlinuz"
		if arch == "s390x" && rhelVersion < 96 {
			artifact = "kernel.img"
		}
	case "ins-file":
		if arch == "s390x" {
			artifact = "generic.ins"
		} else {
			return "", fmt.Errorf("ins-file is only available for the s390x architecture. Current arch: %s", arch)
		}
	default:
		return "", fmt.Errorf("malformed download path: %s", path)
	}
	return artifact, nil
}

func (b *BootArtifactsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		log.Error("Only GET and HEAD methods are supported with this endpoint.")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodHead}, ", "))
		return
	}

	version, arch, err := b.parseQueryParams(r.URL.Query())
	if err != nil {
		httpErrorf(w, http.StatusBadRequest, "Failed to parse query parameters: %v", err)
		return
	}

	IsoFileName = b.ImageStore.PathForParams(imagestore.ImageTypeFull, version, arch)
	artifact, err := parseArtifact(r.URL.Path, arch, version)
	if err != nil {
		httpErrorf(w, http.StatusNotFound, "Failed to parse artifact: %v", err)
		return
	}

	file_path := fmt.Sprintf("/images/pxeboot/%s", artifact)
	if artifact == "generic.ins" {
		// s390x only, unlike other artifacts this one is at the root of the ISO
		file_path = fmt.Sprintf("/%s", artifact)
	}

	fileReader, err := isoeditor.GetFileFromISO(IsoFileName, file_path)
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "Error creating file reader stream: %v", err)
		return
	}
	defer fileReader.Close()

	fileInfo, err := os.Stat(IsoFileName)
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "Error reading file info for %s", IsoFileName)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", artifact))
	http.ServeContent(w, r, artifact, fileInfo.ModTime(), fileReader)
}

func (b *BootArtifactsHandler) parseQueryParams(values url.Values) (string, string, error) {
	version := values.Get("version")
	if version == "" {
		return "", "", fmt.Errorf("'version' parameter required")
	}
	arch := values.Get("arch")
	if arch == "" {
		arch = defaultArch
	}

	if !b.ImageStore.HaveVersion(version, arch) {
		return "", "", fmt.Errorf("version for %s %s, not found", version, arch)
	}

	return version, arch, nil
}

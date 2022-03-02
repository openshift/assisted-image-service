package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
)

type BootArtifactsHandler struct {
	ImageStore imagestore.ImageStore
}

var _ http.Handler = &BootArtifactsHandler{}

var bootpathRegexp = regexp.MustCompile(`^/boot-artifacts/(.+)`)

func parseArtifact(path string) (string, error) {
	match := bootpathRegexp.FindStringSubmatch(path)
	if len(match) < 1 {
		return "", fmt.Errorf("malformed download path: %s", path)
	}

	var artifact string
	switch match[1] {
	case "rootfs":
		artifact = "rootfs.img"
	case "kernel":
		artifact = "vmlinuz"
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

	artifact, err := parseArtifact(r.URL.Path)
	if err != nil {
		log.Errorf("failed to parse artifact: %v\n", err)
		http.NotFound(w, r)
		return
	}
	version, arch, err := b.parseQueryParams(r.URL.Query())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	isoFileName := b.ImageStore.PathForParams(imagestore.ImageTypeFull, version, arch)
	fileReader, err := isoeditor.GetFileFromISO(isoFileName, fmt.Sprintf("/images/pxeboot/%s", artifact))
	if err != nil {
		log.Errorf("Error creating file reader stream: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer fileReader.Close()

	fileInfo, err := os.Stat(isoFileName)
	if err != nil {
		log.Errorf("Error reading file info for %s", isoFileName)
		w.WriteHeader(http.StatusInternalServerError)
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

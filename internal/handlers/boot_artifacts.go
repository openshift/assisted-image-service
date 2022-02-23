package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

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
	if r.Method != "GET" {
		log.Error("Only GET method is supported with this endpoint.")
		http.NotFound(w, r)
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

	modTime := time.Now()
	fileInfo, err := os.Stat(isoFileName)
	if err != nil {
		log.Errorf("Error reading file info for %s", isoFileName)
	} else {
		modTime = fileInfo.ModTime()
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", artifact))
	http.ServeContent(w, r, artifact, modTime, fileReader)
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

	return version, arch, nil
}

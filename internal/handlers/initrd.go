package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/openshift/assisted-image-service/pkg/overlay"
	log "github.com/sirupsen/logrus"
)

type initrdHandler struct {
	ImageStore imagestore.ImageStore
	client     *AssistedServiceClient
}

var _ http.Handler = &initrdHandler{}

func (h *initrdHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	imageID, err := parseImageID(r.URL.Path)
	if err != nil {
		httpErrorf(w, http.StatusNotFound, "failed to parse image ID: %v\n", err)
		return
	}

	version := r.URL.Query().Get("version")
	if version == "" {
		httpErrorf(w, http.StatusBadRequest, "'version' parameter required for initrd download")
		return
	}

	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = defaultArch
	}

	if !h.ImageStore.HaveVersion(version, arch) {
		httpErrorf(w, http.StatusBadRequest, "version for %s %s, not found", version, arch)
		return
	}

	isoPath := h.ImageStore.PathForParams(imagestore.ImageTypeFull, version, arch)
	fsFile, err := isoeditor.GetFileFromISO(isoPath, "/images/pxeboot/initrd.img")
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "failed to get base initrd: %v", err)
		return
	}

	ignition, lastModified, code, err := h.client.ignitionContent(r, imageID)
	if err != nil {
		httpErrorf(w, code, "Error retrieving ignition content: %v", err)
		return
	}

	ignitionReader, err := ignition.Archive()
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "Failed to create ignition archive: %v", err)
		return
	}

	initrdReader, err := overlay.NewAppendReader(fsFile, ignitionReader)
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "Failed to create append reader for initrd: %v", err)
		return
	}
	defer initrdReader.Close()

	fileName := fmt.Sprintf("%s-initrd.img", imageID)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	http.ServeContent(w, r, fileName, modTime, initrdReader)
}

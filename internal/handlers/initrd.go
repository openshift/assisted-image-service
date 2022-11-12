package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/openshift/assisted-image-service/pkg/overlay"
)

type initrdHandler struct {
	ImageStore imagestore.ImageStore
	client     *AssistedServiceClient
}

var _ http.Handler = &initrdHandler{}

func (h *initrdHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "image_id")

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

	ignition, lastModified, code, err := h.client.ignitionContent(r, imageID, "")
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

	ramdisk, statusCode, err := h.client.ramdiskContent(r, imageID)
	if err != nil {
		log.Errorf("Error retrieving ramdisk content: %v\n", err)
		w.WriteHeader(statusCode)
		return
	}
	// the content will be nil if no static networking is configured
	if ramdisk != nil {
		initrdReader, err = overlay.NewAppendReader(initrdReader, bytes.NewReader(ramdisk))
		if err != nil {
			httpErrorf(w, http.StatusInternalServerError, "Failed to create append reader for initrd: %v", err)
			return
		}
	}

	fileName := fmt.Sprintf("%s-initrd.img", imageID)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	http.ServeContent(w, r, fileName, modTime, initrdReader)
}

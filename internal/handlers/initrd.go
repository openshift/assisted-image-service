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
		log.Errorf("failed to parse image ID: %v\n", err)
		http.NotFound(w, r)
		return
	}

	version := r.URL.Query().Get("version")
	if version == "" {
		w.WriteHeader(http.StatusBadRequest)
		errStr := "'version' parameter required for initrd download"
		log.Errorf(errStr)
		_, err = w.Write([]byte(errStr))
		if err != nil {
			log.Errorf("failed to write response: %v", err)
		}
		return
	}

	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = defaultArch
	}

	if !h.ImageStore.HaveVersion(version, arch) {
		w.WriteHeader(http.StatusBadRequest)
		errStr := fmt.Sprintf("version for %s %s, not found", version, arch)
		log.Errorf(errStr)
		_, err = w.Write([]byte(errStr))
		if err != nil {
			log.Errorf("failed to write response: %v", err)
		}
		return
	}

	isoPath := h.ImageStore.PathForParams(imagestore.ImageTypeFull, version, arch)
	fsFile, err := isoeditor.GetFileFromISO(isoPath, "/images/pxeboot/initrd.img")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("failed to get base initrd: %v", err)
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			log.Errorf("failed to write response: %v", err)
		}
		return
	}

	ignition, code, err := h.client.ignitionContent(r, imageID)
	if err != nil {
		log.Errorf("Error retrieving ignition content: %v", err)
		w.WriteHeader(code)
		return
	}

	ignitionReader, err := ignition.Archive()
	if err != nil {
		log.Errorf("Failed to create ignition archive: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	initrdReader, err := overlay.NewAppendReader(fsFile, ignitionReader)
	if err != nil {
		log.Errorf("Failed to create append reader for initrd: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer initrdReader.Close()

	fileName := fmt.Sprintf("%s-initrd.img", imageID)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	http.ServeContent(w, r, fileName, time.Now(), initrdReader)
}

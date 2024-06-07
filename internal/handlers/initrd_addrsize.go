package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

type initrdAddrSizeHandler struct {
	ImageStore imagestore.ImageStore
	client     *AssistedServiceClient
}

var _ http.Handler = &initrdAddrSizeHandler{}

func (h *initrdAddrSizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	imageID := chi.URLParam(r, "image_id")

	version := r.URL.Query().Get("version")
	if version == "" {
		httpErrorf(w, http.StatusBadRequest, "'version' parameter required for initrd download")
		return
	}

	isoPath := h.ImageStore.PathForParams(imagestore.ImageTypeFull, version, "s390x")

	initrdReader, lastModified, code, err := initrdOverlayReader(h.ImageStore, h.client, r, "s390x")
	if err != nil {
		httpErrorf(w, code, err.Error())
		return
	}
	defer initrdReader.Close()

	fileName := fmt.Sprintf("%s-initrd.addrsize", imageID)
	newAddrsizeFile, err := isoeditor.NewInitrdAddrsizeReaderFromISO(isoPath, initrdReader)
	if err != nil {
		log.Errorf("Error calculate initrd.addsize file: %v, isoPath; %s\n", err, isoPath)
		httpErrorf(w, http.StatusInternalServerError, "Failed to get initrd.addrsize: %v", err)
		return
	}

	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))

	http.ServeContent(w, r, fileName, modTime, newAddrsizeFile)
}

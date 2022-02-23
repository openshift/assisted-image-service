package handlers

import (
	"net/http"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

const defaultArch = "x86_64"

type ImageHandler struct {
	isos http.Handler
	sem  *semaphore.Weighted
}

var _ http.Handler = &ImageHandler{}

func NewImageHandler(is imagestore.ImageStore, assistedServiceClient *AssistedServiceClient, maxRequests int64) http.Handler {
	isos := &isoHandler{
		ImageStore:          is,
		GenerateImageStream: isoeditor.NewRHCOSStreamReader,
		client:              assistedServiceClient,
	}

	h := &ImageHandler{
		isos: isos,
		sem:  semaphore.NewWeighted(maxRequests),
	}
}

func (h *ImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.sem.Acquire(r.Context(), 1); err != nil {
		log.Errorf("Failed to acquire semaphore: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer h.sem.Release(1)

	h.isos.ServeHTTP(w, r)
}

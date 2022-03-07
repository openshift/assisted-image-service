package handlers

import (
	"net/http"
	"regexp"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
	"github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"
	"golang.org/x/sync/semaphore"
)

const defaultArch = "x86_64"

type ImageHandler struct {
	iso    http.Handler
	initrd http.Handler
	sem    *semaphore.Weighted
}

var _ http.Handler = &ImageHandler{}

func NewImageHandler(is imagestore.ImageStore, assistedServiceClient *AssistedServiceClient, maxRequests int64, mdw middleware.Middleware) http.Handler {
	isos := &isoHandler{
		ImageStore:          is,
		GenerateImageStream: isoeditor.NewRHCOSStreamReader,
		client:              assistedServiceClient,
	}
	initrds := &initrdHandler{
		ImageStore: is,
		client:     assistedServiceClient,
	}

	return &ImageHandler{
		iso:    stdmiddleware.Handler("/images/:imageID", mdw, isos),
		initrd: stdmiddleware.Handler("/images/:imageID/pxe-initrd", mdw, initrds),
		sem:    semaphore.NewWeighted(maxRequests),
	}
}

func (h *ImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.sem.Acquire(r.Context(), 1); err != nil {
		log.Errorf("Failed to acquire semaphore: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer h.sem.Release(1)

	matched, err := regexp.MatchString(`/images/.*/pxe-initrd`, r.URL.Path)
	if err != nil {
		log.Errorf("Failed to test path match to initrd: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if matched {
		h.initrd.ServeHTTP(w, r)
	} else {
		h.iso.ServeHTTP(w, r)
	}
}

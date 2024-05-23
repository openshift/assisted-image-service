package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	metricsmiddleware "github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

const defaultArch = "x86_64"

type ImageHandler struct {
	long                http.Handler
	byAPIKey            http.Handler
	byID                http.Handler
	byToken             http.Handler
	initrd              http.Handler
	s390xInitrdAddrsize http.Handler
}

func NewImageHandler(is imagestore.ImageStore, assistedServiceClient *AssistedServiceClient, maxRequests int64, mdw metricsmiddleware.Middleware) http.Handler {
	h := ImageHandler{
		long: stdmiddleware.Handler("/images/:imageID", mdw,
			&isoHandler{
				ImageStore:          is,
				GenerateImageStream: isoeditor.NewRHCOSStreamReader,
				client:              assistedServiceClient,
				urlParser:           parseLongURL,
			},
		),
		byAPIKey: stdmiddleware.Handler("/byapikey/:token", mdw,
			&isoHandler{
				ImageStore:          is,
				GenerateImageStream: isoeditor.NewRHCOSStreamReader,
				client:              assistedServiceClient,
				urlParser:           parseShortURL,
			},
		),
		byID: stdmiddleware.Handler("/byid/:token", mdw,
			&isoHandler{
				ImageStore:          is,
				GenerateImageStream: isoeditor.NewRHCOSStreamReader,
				client:              assistedServiceClient,
				urlParser:           parseShortURL,
			},
		),
		byToken: stdmiddleware.Handler("/bytoken/:token", mdw,
			&isoHandler{
				ImageStore:          is,
				GenerateImageStream: isoeditor.NewRHCOSStreamReader,
				client:              assistedServiceClient,
				urlParser:           parseShortURL,
			},
		),
		initrd: stdmiddleware.Handler("/images/:imageID/pxe-initrd", mdw,
			&initrdHandler{
				ImageStore: is,
				client:     assistedServiceClient,
			},
		),
		s390xInitrdAddrsize: stdmiddleware.Handler("/images/:imageID/s390x-initrd-addrsize", mdw,
			&initrdAddrSizeHandler{
				ImageStore: is,
				client:     assistedServiceClient,
			},
		),
	}

	return h.router(maxRequests)
}

func (h *ImageHandler) router(maxRequests int64) *chi.Mux {
	router := chi.NewRouter()
	router.Use(WithRequestLimit(maxRequests))
	router.Handle("/images/{image_id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}/pxe-initrd", h.initrd)
	router.Handle("/images/{image_id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}/s390x-initrd-addrsize", h.s390xInitrdAddrsize)
	router.Handle("/images/{image_id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}", h.long)
	router.Handle("/byid/{image_id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}/{version}/{arch}/{filename}", h.byID)
	router.Handle("/byapikey/{api_key}/{version}/{arch}/{filename}", h.byAPIKey)
	router.Handle("/bytoken/{token}/{version}/{arch}/{filename}", h.byToken)

	return router
}

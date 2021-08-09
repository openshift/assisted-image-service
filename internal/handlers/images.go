package handlers

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	log "github.com/sirupsen/logrus"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"
)

type ImageHandler struct {
	ImageStore imagestore.ImageStore
}

var _ http.Handler = &ImageHandler{}

var clusterRegexp = regexp.MustCompile(`^/images/(.+)`)

func parseImageID(path string) (string, error) {
	match := clusterRegexp.FindStringSubmatch(path)
	if match == nil {
		return "", fmt.Errorf("malformed download path: %s", path)
	}
	return match[1], nil
}

func NewImageHandler(is imagestore.ImageStore) http.Handler {
	metricsConfig := metrics.Config{
		Prefix:          "assisted_image_service",
		DurationBuckets: []float64{.1, 1, 10, 50, 100, 300, 600},
		SizeBuckets:     []float64{100, 1e6, 5e8, 1e9, 1e10},
	}
	mdw := middleware.New(middleware.Config{
		Recorder: metrics.NewRecorder(metricsConfig),
	})

	return stdmiddleware.Handler("/images/:imageID", mdw, &ImageHandler{ImageStore: is})
}

func (h *ImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clusterID, err := parseImageID(r.URL.Path)
	if err != nil {
		log.Errorf("failed to parse cluster ID: %v\n", err)
		http.NotFound(w, r)
		return
	}

	params := r.URL.Query()

	version := params.Get("version")
	if version == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte("'version' parameter required"))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	if !h.ImageStore.HaveVersion(version) {
		w.WriteHeader(http.StatusBadRequest)
		message := fmt.Sprintf("version %s not found", version)
		_, err = w.Write([]byte(message))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	imageType := params.Get("type")
	if imageType == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte("'type' parameter required"))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	} else if imageType != imagestore.ImageTypeFull && imageType != imagestore.ImageTypeMinimal {
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte("invalid value for parameter 'type'"))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	f, err := h.ImageStore.BaseFile(version, imageType)
	if err != nil {
		log.Errorf("Error getting base image: err: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		log.Errorf("Error getting file info: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fileName := fmt.Sprintf("%s-discovery.iso", clusterID)
	http.ServeContent(w, r, fileName, fileInfo.ModTime(), f)
}

package handlers

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/cavaliercoder/go-cpio"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"
)

const (
	RequestAuthTypeHeader = "header"
	RequestAuthTypeParam  = "param"
)

const (
	fileRouteFormat = "/api/assisted-install/v2/infra-envs/%s/downloads/files"
)

type ImageHandler struct {
	ImageStore            imagestore.ImageStore
	AssistedServiceScheme string
	AssistedServiceHost   string
	GenerateImageStream   isoeditor.StreamGeneratorFunc
	RequestAuthType       string
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

func NewImageHandler(is imagestore.ImageStore, assistedServiceScheme, assistedServiceHost, requestAuthType string) http.Handler {
	metricsConfig := metrics.Config{
		Prefix:          "assisted_image_service",
		DurationBuckets: []float64{.1, 1, 10, 50, 100, 300, 600},
		SizeBuckets:     []float64{100, 1e6, 5e8, 1e9, 1e10},
	}
	mdw := middleware.New(middleware.Config{
		Recorder: metrics.NewRecorder(metricsConfig),
	})

	h := &ImageHandler{
		ImageStore:            is,
		AssistedServiceScheme: assistedServiceScheme,
		AssistedServiceHost:   assistedServiceHost,
		GenerateImageStream:   isoeditor.NewRHCOSStreamReader,
		RequestAuthType:       requestAuthType,
	}

	return stdmiddleware.Handler("/images/:imageID", mdw, h)
}

func (h *ImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clusterID, err := parseImageID(r.URL.Path)
	if err != nil {
		log.Errorf("failed to parse cluster ID: %v\n", err)
		http.NotFound(w, r)
		return
	}

	version, imageType, apiKey, err := h.parseQueryParams(r.URL.Query())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	isoReader, err := h.imageStreamForID(clusterID, version, imageType, apiKey)
	if err != nil {
		log.Errorf("Error creating image stream: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	//TODO: set modified time correctly (MGMT-7274)

	fileName := fmt.Sprintf("%s-discovery.iso", clusterID)
	http.ServeContent(w, r, fileName, time.Now(), isoReader)
}

func (h *ImageHandler) parseQueryParams(values url.Values) (string, string, string, error) {
	version := values.Get("version")
	if version == "" {
		return "", "", "", fmt.Errorf("'version' parameter required")
	}

	if !h.ImageStore.HaveVersion(version) {
		return "", "", "", fmt.Errorf("version %s not found", version)
	}

	imageType := values.Get("type")
	if imageType == "" {
		return "", "", "", fmt.Errorf("'type' parameter required")
	} else if imageType != imagestore.ImageTypeFull && imageType != imagestore.ImageTypeMinimal {
		return "", "", "", fmt.Errorf("invalid value '%s' for parameter 'type'", imageType)
	}

	apiKey := values.Get("api_key")

	return version, imageType, apiKey, nil
}

func (h *ImageHandler) imageStreamForID(imageID, version, imageType, apiKey string) (io.ReadSeeker, error) {
	ignition, err := h.ignitionContent(imageID, apiKey)
	if err != nil {
		return nil, err
	}

	var ramdisk []byte
	if imageType == imagestore.ImageTypeMinimal {
		ramdisk, err = h.ramdiskContent(imageID, apiKey)
		if err != nil {
			return nil, err
		}
	}

	return h.GenerateImageStream(h.ImageStore.PathForParams(imageType, version, "x86_64"), ignition, ramdisk)
}

func (h *ImageHandler) ramdiskContent(imageID, apiKey string) ([]byte, error) {
	var ramdiskBytes []byte
	if h.AssistedServiceHost == "" {
		return nil, nil
	}

	u := url.URL{
		Scheme: h.AssistedServiceScheme,
		Host:   h.AssistedServiceHost,
		Path:   fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID),
	}
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	err = h.setRequestAuth(req, apiKey)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request to %s returned status %d: %v", u.String(), resp.StatusCode, err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	ramdiskBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return ramdiskBytes, nil
}

func (h *ImageHandler) setRequestAuth(req *http.Request, apiKey string) error {
	switch h.RequestAuthType {
	case RequestAuthTypeParam:
		params := req.URL.Query()
		params.Set("api_key", apiKey)
		req.URL.RawQuery = params.Encode()
	case RequestAuthTypeHeader:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "":
	default:
		return fmt.Errorf("invalid request auth type '%s'", h.RequestAuthType)
	}
	return nil
}

func (h *ImageHandler) ignitionContent(imageID string, apiKey string) ([]byte, error) {
	if h.AssistedServiceHost == "" {
		return nil, nil
	}

	u := url.URL{
		Scheme: h.AssistedServiceScheme,
		Host:   h.AssistedServiceHost,
		Path:   fmt.Sprintf(fileRouteFormat, imageID),
	}
	queryValues := url.Values{}
	queryValues.Set("file_name", "discovery.ign")
	u.RawQuery = queryValues.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	err = h.setRequestAuth(req, apiKey)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ignition request to %s returned status %d: %v", req.URL.String(), resp.StatusCode, err)
	}
	ignitionBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	// Create CPIO archive
	archiveBuffer := new(bytes.Buffer)
	cpioWriter := cpio.NewWriter(archiveBuffer)
	if err := cpioWriter.WriteHeader(&cpio.Header{Name: "config.ign", Mode: 0o100_644, Size: int64(len(ignitionBytes))}); err != nil {
		return nil, errors.Wrap(err, "Failed to write CPIO header")
	}
	if _, err := cpioWriter.Write(ignitionBytes); err != nil {

		return nil, errors.Wrap(err, "Failed to write CPIO archive")
	}
	if err := cpioWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "Failed to close CPIO archive")
	}

	// Run gzip compression
	compressedBuffer := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(compressedBuffer)
	if _, err := gzipWriter.Write(archiveBuffer.Bytes()); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip archive")
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip archive")
	}

	return compressedBuffer.Bytes(), nil
}

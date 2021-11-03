package handlers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"
	"golang.org/x/sync/semaphore"
)

const (
	fileRouteFormat = "/api/assisted-install/v2/infra-envs/%s/downloads/files"
	defaultArch     = "x86_64"
)

type ImageHandler struct {
	ImageStore            imagestore.ImageStore
	AssistedServiceScheme string
	AssistedServiceHost   string
	GenerateImageStream   isoeditor.StreamGeneratorFunc
	Client                *http.Client
	sem                   *semaphore.Weighted
}

type imageDownloadParams struct {
	version   string
	imageType string
	arch      string
}

var _ http.Handler = &ImageHandler{}

var pathRegexp = regexp.MustCompile(`^/images/(.+)`)

func parseImageID(path string) (string, error) {
	match := pathRegexp.FindStringSubmatch(path)
	if match == nil {
		return "", fmt.Errorf("malformed download path: %s", path)
	}
	return match[1], nil
}

func NewImageHandler(is imagestore.ImageStore, reg *prometheus.Registry, assistedServiceScheme, assistedServiceHost, caCertFile string, maxRequests int64) http.Handler {
	metricsConfig := metrics.Config{
		Registry:        reg,
		Prefix:          "assisted_image_service",
		DurationBuckets: []float64{.1, 1, 10, 50, 100, 300, 600},
		SizeBuckets:     []float64{100, 1e6, 5e8, 1e9, 1e10},
	}
	mdw := middleware.New(middleware.Config{
		Recorder: metrics.NewRecorder(metricsConfig),
	})

	client := &http.Client{}
	if caCertFile != "" {
		caCert, err := ioutil.ReadFile(caCertFile)
		if err != nil {
			log.Fatalf("Error opening cert file %s, %s", caCertFile, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			log.Fatalf("Failed to append cert %s, %s", caCertFile, err)
		}

		t := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS12,
			},
		}
		client.Transport = t
	}

	h := &ImageHandler{
		ImageStore:            is,
		AssistedServiceScheme: assistedServiceScheme,
		AssistedServiceHost:   assistedServiceHost,
		GenerateImageStream:   isoeditor.NewRHCOSStreamReader,
		Client:                client,
		sem:                   semaphore.NewWeighted(maxRequests),
	}

	return stdmiddleware.Handler("/images/:imageID", mdw, h)
}

func (h *ImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.sem.Acquire(r.Context(), 1); err != nil {
		log.Errorf("Failed to acquire semaphore: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer h.sem.Release(1)

	imageID, err := parseImageID(r.URL.Path)
	if err != nil {
		log.Errorf("failed to parse image ID: %v\n", err)
		http.NotFound(w, r)
		return
	}

	params, err := h.parseQueryParams(r.URL.Query())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	ignition, err := h.ignitionContent(r, imageID)
	if err != nil {
		log.Errorf("Error retrieving ignition content: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var ramdisk []byte
	if params.imageType == imagestore.ImageTypeMinimal {
		ramdisk, err = h.ramdiskContent(r, imageID)
		if err != nil {
			log.Errorf("Error retrieving ramdisk content: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	isoReader, err := h.GenerateImageStream(h.ImageStore.PathForParams(params.imageType, params.version, params.arch), ignition, ramdisk)
	if err != nil {
		log.Errorf("Error creating image stream: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	//TODO: set modified time correctly (MGMT-7274)

	fileName := fmt.Sprintf("%s-discovery.iso", imageID)
	http.ServeContent(w, r, fileName, time.Now(), isoReader)
}

func (h *ImageHandler) parseQueryParams(values url.Values) (*imageDownloadParams, error) {
	version := values.Get("version")
	if version == "" {
		return nil, fmt.Errorf("'version' parameter required")
	}

	arch := values.Get("arch")
	if arch == "" {
		arch = defaultArch
	}

	if !h.ImageStore.HaveVersion(version, arch) {
		return nil, fmt.Errorf("version for %s %s, not found", version, arch)
	}

	imageType := values.Get("type")
	if imageType == "" {
		return nil, fmt.Errorf("'type' parameter required")
	} else if imageType != imagestore.ImageTypeFull && imageType != imagestore.ImageTypeMinimal {
		return nil, fmt.Errorf("invalid value '%s' for parameter 'type'", imageType)
	}

	return &imageDownloadParams{
		version:   version,
		imageType: imageType,
		arch:      arch,
	}, nil
}

func (h *ImageHandler) ramdiskContent(imageServiceRequest *http.Request, imageID string) ([]byte, error) {
	var ramdiskBytes []byte
	if h.AssistedServiceHost == "" {
		return nil, nil
	}

	u := url.URL{
		Scheme: h.AssistedServiceScheme,
		Host:   h.AssistedServiceHost,
		Path:   fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID),
	}
	req, err := http.NewRequestWithContext(imageServiceRequest.Context(), "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	h.setRequestAuth(imageServiceRequest, req)

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request to %s returned status %d", u.String(), resp.StatusCode)
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

func (h *ImageHandler) ignitionContent(imageServiceRequest *http.Request, imageID string) (*isoeditor.IgnitionContent, error) {
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

	req, err := http.NewRequestWithContext(imageServiceRequest.Context(), "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	h.setRequestAuth(imageServiceRequest, req)

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ignition request to %s returned status %d", req.URL.String(), resp.StatusCode)
	}
	ignitionBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return &isoeditor.IgnitionContent{Config: ignitionBytes}, nil
}

func (h *ImageHandler) setRequestAuth(imageRequest, assistedRequest *http.Request) {
	queryValues := imageRequest.URL.Query()
	authHeader := imageRequest.Header.Get("Authorization")

	if queryValues.Get("api_key") != "" {
		params := assistedRequest.URL.Query()
		params.Set("api_key", queryValues.Get("api_key"))
		assistedRequest.URL.RawQuery = params.Encode()
	} else if queryValues.Get("image_token") != "" {
		assistedRequest.Header.Set("Image-Token", queryValues.Get("image_token"))
	} else if authHeader != "" {
		assistedRequest.Header.Set("Authorization", authHeader)
	}
}

package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
)

type isoHandler struct {
	ImageStore          imagestore.ImageStore
	GenerateImageStream isoeditor.StreamGeneratorFunc
	client              *AssistedServiceClient
}

var _ http.Handler = &isoHandler{}

type imageDownloadParams struct {
	version   string
	imageType string
	arch      string
}

func (h *isoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	ignition, lastModified, statusCode, err := h.client.ignitionContent(r, imageID, params.imageType)
	if err != nil {
		log.Errorf("Error retrieving ignition content: %v\n", err)
		w.WriteHeader(statusCode)
		return
	}

	var ramdisk []byte
	if params.imageType == imagestore.ImageTypeMinimal {
		ramdisk, statusCode, err = h.client.ramdiskContent(r, imageID)
		if err != nil {
			log.Errorf("Error retrieving ramdisk content: %v\n", err)
			w.WriteHeader(statusCode)
			return
		}
	}

	isoReader, err := h.GenerateImageStream(h.ImageStore.PathForParams(params.imageType, params.version, params.arch), ignition, ramdisk)
	if err != nil {
		log.Errorf("Error creating image stream: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer isoReader.Close()

	fileName := fmt.Sprintf("%s-discovery.iso", imageID)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	http.ServeContent(w, r, fileName, modTime, isoReader)
}

func (h *isoHandler) parseQueryParams(values url.Values) (*imageDownloadParams, error) {
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

var pathRegexp = regexp.MustCompile(`^/images/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(/pxe-initrd)?`)

func parseImageID(path string) (string, error) {
	match := pathRegexp.FindStringSubmatch(path)
	if match == nil {
		return "", fmt.Errorf("malformed download path: %s", path)
	}
	return match[1], nil
}

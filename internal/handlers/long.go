package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
)

// parseLongURL parses the long-style URLs that use query parameters to identify
// the desired resource. This style of URL is deprecated in favor of short URLs.
func parseLongURL(r *http.Request) (*imageDownloadParams, int, error) {
	imageID := chi.URLParam(r, "image_id")

	values := r.URL.Query()
	version := values.Get("version")
	if version == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("'version' parameter required")
	}

	arch := values.Get("arch")
	if arch == "" {
		arch = defaultArch
	}

	imageType := values.Get("type")
	if imageType == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("'type' parameter required")
	} else if imageType != imagestore.ImageTypeFull && imageType != imagestore.ImageTypeMinimal {
		return nil, http.StatusBadRequest, fmt.Errorf("invalid value '%s' for parameter 'type'", imageType)
	}

	return &imageDownloadParams{
		version:   version,
		imageType: imageType,
		arch:      arch,
		imageID:   imageID,
	}, 0, nil
}

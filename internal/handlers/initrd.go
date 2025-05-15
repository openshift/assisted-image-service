package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/openshift/assisted-image-service/internal/common"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/openshift/assisted-image-service/pkg/overlay"
)

type initrdHandler struct {
	ImageStore imagestore.ImageStore
	client     *AssistedServiceClient
}

var _ http.Handler = &initrdHandler{}

func (h *initrdHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "image_id")

	version := r.URL.Query().Get("version")
	if version == "" {
		httpErrorf(w, http.StatusBadRequest, "'version' parameter required for initrd download")
		return
	}

	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = defaultArch
	}

	initrdReader, lastModified, code, err := initrdOverlayReader(h.ImageStore, h.client, r, arch)
	if err != nil {
		httpErrorf(w, code, err.Error())
		return
	}
	defer initrdReader.Close()

	fileName := fmt.Sprintf("%s-initrd.img", imageID)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	http.ServeContent(w, r, fileName, modTime, initrdReader)
}

func initrdOverlayReader(imageStore imagestore.ImageStore, client *AssistedServiceClient, r *http.Request, arch string) (overlay.OverlayReader, string, int, error) {
	imageID := chi.URLParam(r, "image_id")

	version := r.URL.Query().Get("version")
	if version == "" {
		return nil, "", http.StatusBadRequest, fmt.Errorf("'version' parameter required for initrd download")
	}

	// check if image is available for given version and architecture
	if !imageStore.HaveVersion(version, arch) {
		return nil, "", http.StatusBadRequest, fmt.Errorf("version for %s %s, not found ", version, arch)
	}

	isoPath := imageStore.PathForParams(imagestore.ImageTypeFull, version, arch)

	ignition, lastModified, code, err := client.ignitionContent(r, imageID, "")
	if err != nil {
		return nil, "", code, fmt.Errorf("error retrieving ignition content: %v", err)
	}

	initrdReader, err := isoeditor.NewInitRamFSStreamReaderFromISO(isoPath, ignition)
	if err != nil {
		return nil, "", http.StatusInternalServerError, fmt.Errorf("failed to get initrd: %v", err)
	}

	ramdisk, statusCode, err := client.ramdiskContent(r, imageID)
	if err != nil {
		return nil, "", statusCode, fmt.Errorf("error retrieving ramdisk content: %v", err)
	}

	// the content will be nil if no static networking is configured
	if ramdisk != nil {
		initrdReader, err = overlay.NewAppendReader(initrdReader, bytes.NewReader(ramdisk))
		if err != nil {
			return nil, "", http.StatusInternalServerError, fmt.Errorf("failed to create append reader for initrd: %v", err)
		}

		versionOK, err := common.VersionGreaterOrEqual(version, isoeditor.MinimalVersionForNmstatectl)
		if err != nil {
			return nil, "", http.StatusInternalServerError, err
		}

		if versionOK {
			nmstatectlPath, err := imageStore.NmstatectlPathForParams(version, arch)
			if err != nil {
				return nil, "", http.StatusInternalServerError, err
			}
			nmstateImgContent, err := os.Open(nmstatectlPath)
			if err != nil {
				return nil, "", http.StatusInternalServerError, fmt.Errorf("failed to read nmstate img: %v", err)
			}
			initrdReader, err = overlay.NewAppendReader(initrdReader, nmstateImgContent)
			if err != nil {
				return nil, "", http.StatusInternalServerError, fmt.Errorf("failed to create append reader for initrd: %v", err)
			}
		}
	}

	return initrdReader, lastModified, 0, nil
}

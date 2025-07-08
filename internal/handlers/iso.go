package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
)

type isoHandler struct {
	ImageStore          imagestore.ImageStore
	GenerateImageStream isoeditor.StreamGeneratorFunc
	client              *AssistedServiceClient
	// second arg is an HTTP response code to use when the error != nil
	urlParser func(*http.Request) (*imageDownloadParams, int, error)
}

var _ http.Handler = &isoHandler{}

type imageDownloadParams struct {
	imageID   string
	version   string
	imageType string
	arch      string
}

func (h *isoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	params, statusCode, err := h.urlParser(r)

	if err != nil {
		w.WriteHeader(statusCode)
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			log.Errorf("Failed to write response: %v\n", err)
		}
		return
	}

	if !h.ImageStore.HaveVersion(params.version, params.arch) {
		log.Errorf("version for %s %s, not found", params.version, params.arch)
		http.NotFound(w, r)
		return
	}

	isOVE := h.ImageStore.IsOVEImage(params.version, params.arch)
	ignition, lastModified, statusCode, err := h.client.ignitionContent(r, params.imageID, params.imageType, isOVE)
	if err != nil {
		log.Errorf("Error retrieving ignition content: %v\n", err)
		w.WriteHeader(statusCode)
		return
	}

	var ramdisk []byte
	if params.imageType == imagestore.ImageTypeMinimal {
		ramdisk, statusCode, err = h.client.ramdiskContent(r, params.imageID)
		if err != nil {
			log.Errorf("Error retrieving ramdisk content: %v\n", err)
			w.WriteHeader(statusCode)
			return
		}
	}

	var kargs []byte
	kargs, statusCode, err = h.client.discoveryKernelArguments(r, params.imageID)
	if err != nil {
		log.Errorf("Error retrieving kernel arguments content: %v\n", err)
		w.WriteHeader(statusCode)
		return
	}

	if kargs != nil && params.arch == "s390x" {
		httpErrorf(w, http.StatusBadRequest, "kargs cannot be modified in s390x architecture ISOs")
		return
	}

	isoReader, err := h.GenerateImageStream(h.ImageStore.PathForParams(params.imageType, params.version, params.arch), ignition, ramdisk, kargs)
	if err != nil {
		log.Errorf("Error creating image stream: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer isoReader.Close()

	fileName := fmt.Sprintf("%s-discovery.iso", params.imageID)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	http.ServeContent(w, r, fileName, modTime, isoReader)
}

package handlers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

const initrdAddrsizePathInISO = "images/initrd.addrsize"

type initrdAddrSizeHandler struct {
	ImageStore imagestore.ImageStore
	client     *AssistedServiceClient
}

var _ http.Handler = &initrdAddrSizeHandler{}

func (h *initrdAddrSizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	imageID := chi.URLParam(r, "image_id")

	version := r.URL.Query().Get("version")
	if version == "" {
		httpErrorf(w, http.StatusBadRequest, "'version' parameter required for initrd download")
		return
	}

	isoPath := h.ImageStore.PathForParams(imagestore.ImageTypeFull, version, "s390x")

	initrdReader, lastModified, code, err := initrdOverlayReader(h.ImageStore, h.client, r, "s390x")
	if err != nil {
		httpErrorf(w, code, err.Error())
		return
	}
	defer initrdReader.Close()

	fileName := fmt.Sprintf("%s-initrd.addrsize", imageID)
	addrsizeReader, err := isoeditor.GetFileFromISO(isoPath, initrdAddrsizePathInISO)
	if err != nil {
		log.Errorf("Error retrieving initrd.addsize file: %v, isoPath; %s\n", err, isoPath)
		httpErrorf(w, http.StatusInternalServerError, "Failed to get initrd.addrsize: %v", err)
		return
	}
	defer addrsizeReader.Close()

	// get the size of the initrd including the embedded ignition
	sizeOfInitrd, err := initrdReader.Seek(0, io.SeekEnd)
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "Failed to determine size of initrd: %v", err)
		return
	}

	addrsizeBytes := new(bytes.Buffer)
	err = binary.Write(addrsizeBytes, binary.BigEndian, sizeOfInitrd)
	if err != nil {
		httpErrorf(w, http.StatusInternalServerError, "Error during write buffer: %v", err)
		return
	}
	initrdPSW := make([]byte, 8)
	m, err := addrsizeReader.Read(initrdPSW)
	if err != nil || m != 8 {
		log.Errorf("Error reading initrd.addsize file: %v, isoPath; %s\n", err, isoPath)
		httpErrorf(w, http.StatusInternalServerError, "Failed to read initrd.addrsize: %v", err)
		return
	}

	modTime, err := http.ParseTime(lastModified)
	if err != nil {
		log.Warnf("Error parsing last modified time %s: %v", lastModified, err)
		modTime = time.Now()
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))

	http.ServeContent(w, r, fileName, modTime, bytes.NewReader(append(initrdPSW, addrsizeBytes.Bytes()...)))
}

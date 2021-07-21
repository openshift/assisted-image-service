package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/carbonin/assisted-image-service/pkg/imagestore"
)

type ImageHandler struct {
	ImageStore *imagestore.ImageStore
}

var _ http.Handler = &ImageHandler{}

var clusterRegexp = regexp.MustCompile(`/images/.+`)

func parseClusterID(path string) (string, error) {
	found := clusterRegexp.FindString(path)
	if found == "" {
		return "", fmt.Errorf("malformed download path: %s", path)
	}
	return found, nil
}

func (h *ImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clusterID, err := parseClusterID(r.URL.Path)
	if err != nil {
		log.Printf("failed to parse cluster ID: %v\n", err)
		http.NotFound(w, r)
		return
	}

	log.Printf("Get info for cluster %s here\n", clusterID)

	// TODO: Make this configurable based on query params
	f, err := h.ImageStore.BaseFile("4.8")
	if err != nil {
		log.Printf("Error getting base image: err: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		log.Printf("Error getting file info: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fileName := fmt.Sprintf("%s-discovery.iso", clusterID)
	http.ServeContent(w, r, fileName, fileInfo.ModTime(), f)
}

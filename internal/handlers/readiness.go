package handlers

import (
	"net/http"

	log "github.com/sirupsen/logrus"
)

type ReadinessHandler struct {
	isEnabled bool
}

func NewReadinessHandler() *ReadinessHandler {
	return &ReadinessHandler{
		isEnabled: false,
	}
}

func (a *ReadinessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !a.isEnabled {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *ReadinessHandler) Enable() {
	a.isEnabled = true
	log.Info("API is enabled")
}

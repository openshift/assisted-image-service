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
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	a.runIfReady(http.HandlerFunc(ok), w, r)
}

func (a *ReadinessHandler) WithMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.runIfReady(next, w, r)
	})
}

func (a *ReadinessHandler) runIfReady(next http.Handler, w http.ResponseWriter, r *http.Request) {
	if !a.isEnabled {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	next.ServeHTTP(w, r)
}

func (a *ReadinessHandler) Enable() {
	a.isEnabled = true
	log.Info("API is enabled")
}

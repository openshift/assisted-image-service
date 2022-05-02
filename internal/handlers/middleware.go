package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/cors"
)

func WithCORSMiddleware(handler http.Handler, domains string) http.Handler {
	domainList := strings.Split(strings.ReplaceAll(domains, " ", ""), ",")

	corsHandler := cors.New(cors.Options{
		Debug: false,
		AllowedMethods: []string{
			http.MethodHead,
			http.MethodGet,
		},
		AllowedOrigins: domainList,
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
		},
		MaxAge: int((10 * time.Minute).Seconds()),
	})
	return corsHandler.Handler(handler)
}

func WithInitrdViaHTTP(handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check plain HTTP requests
		if r.TLS == nil {
			if _, err := parseImageID(r.URL.Path); err != nil {
				// Invalid UUID format
				http.NotFound(w, r)
				return
			}
			if !strings.HasSuffix(r.URL.Path, "/pxe-initrd") {
				// Only "/pxe-initrd" is allowed to be fetched
				http.NotFound(w, r)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}
}

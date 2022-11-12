package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/cors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
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
			if !strings.HasSuffix(r.URL.Path, "/pxe-initrd") {
				// Only "/pxe-initrd" is allowed to be fetched
				http.NotFound(w, r)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}
}

// WithRequestLimit returns middleware that will limit the number of requests
// being concurrently handled to maxRequests. Blocks until a slot becomes
// available. A 503 response will be returned if the context expires or is
// cancelled while waiting.
func WithRequestLimit(maxRequests int64) func(http.Handler) http.Handler {
	sem := semaphore.NewWeighted(maxRequests)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := sem.Acquire(r.Context(), 1); err != nil {
				log.Errorf("Failed to acquire semaphore: %v", err)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			defer sem.Release(1)

			next.ServeHTTP(w, r)
		})
	}
}

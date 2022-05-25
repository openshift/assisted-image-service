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

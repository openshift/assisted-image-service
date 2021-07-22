package main

import (
	"context"
	"log"
	"net/http"

	"github.com/carbonin/assisted-image-service/internal/handlers"
	"github.com/carbonin/assisted-image-service/pkg/imagestore"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Options struct {
	AssistedServiceURL string `envconfig:"ASSISTED_SERVICE_URL" default:"http://assisted-service:8080"`
}

func main() {
	err := envconfig.Process("cluster-image", &Options)
	if err != nil {
		log.Fatalf("Failed to process config: %v\n", err)
	}
	is, err := imagestore.NewImageStore()
	if err != nil {
		log.Fatalf("Failed to create image store: %v\n", err)
	}
	err = is.Populate(context.Background())
	if err != nil {
		log.Fatalf("Failed to populate image store: %v\n", err)
	}

	http.Handle("/images/", handlers.NewImageHandler(is))
	http.Handle("/health", handlers.NewHealthHandler())
	http.Handle("/metrics", promhttp.Handler())

	log.Printf("Starting http handler...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

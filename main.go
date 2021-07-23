package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/carbonin/assisted-image-service/internal/handlers"
	"github.com/carbonin/assisted-image-service/pkg/imagestore"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Options struct {
	AssistedServiceURL string `envconfig:"ASSISTED_SERVICE_URL" default:"http://assisted-service:8080"`
	Port               string `envconfig:"PORT" default:"8080"`
	HTTPSKeyFile       string `envconfig:"HTTPS_KEY_FILE"`
	HTTPSCertFile      string `envconfig:"HTTPS_CERT_FILE"`
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
	address := fmt.Sprintf(":%s", Options.Port)
	if Options.HTTPSKeyFile != "" && Options.HTTPSCertFile != "" {
		log.Fatal(http.ListenAndServeTLS(address, Options.HTTPSCertFile, Options.HTTPSKeyFile, nil))
	} else {
		log.Fatal(http.ListenAndServe(address, nil))
	}
}

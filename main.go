package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/internal/isoeditor"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var Options struct {
	AssistedServiceURL string `envconfig:"ASSISTED_SERVICE_URL" default:"http://assisted-service:8080"`
	Port               string `envconfig:"PORT" default:"8080"`
	HTTPSKeyFile       string `envconfig:"HTTPS_KEY_FILE"`
	HTTPSCertFile      string `envconfig:"HTTPS_CERT_FILE"`
	DataDir            string `envconfig:"DATA_DIR"`
}

func main() {
	log.SetReportCaller(true)
	log.SetFormatter(&log.JSONFormatter{})
	err := envconfig.Process("cluster-image", &Options)
	if err != nil {
		log.Fatalf("Failed to process config: %v\n", err)
	}
	is, err := imagestore.NewImageStore(isoeditor.NewEditor(Options.DataDir), Options.DataDir)
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

	log.Info("Starting http handler...")
	address := fmt.Sprintf(":%s", Options.Port)
	if Options.HTTPSKeyFile != "" && Options.HTTPSCertFile != "" {
		log.Fatal(http.ListenAndServeTLS(address, Options.HTTPSCertFile, Options.HTTPSKeyFile, nil))
	} else {
		log.Fatal(http.ListenAndServe(address, nil))
	}
}

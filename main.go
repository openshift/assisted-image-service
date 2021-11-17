package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var Options struct {
	AssistedServiceScheme string `envconfig:"ASSISTED_SERVICE_SCHEME"`
	AssistedServiceHost   string `envconfig:"ASSISTED_SERVICE_HOST"`
	DataDir               string `envconfig:"DATA_DIR"`
	HTTPSKeyFile          string `envconfig:"HTTPS_KEY_FILE"`
	HTTPSCertFile         string `envconfig:"HTTPS_CERT_FILE"`
	HTTPSCAFile           string `envconfig:"HTTPS_CA_FILE"`
	ListenPort            string `envconfig:"LISTEN_PORT" default:"8080"`
	RequestAuthType       string `envconfig:"REQUEST_AUTH_TYPE"`
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

	readinessHandler := handlers.NewReadinessHandler()

	go func() {
		err = is.Populate(context.Background())
		if err != nil {
			log.Fatalf("Failed to populate image store: %v\n", err)
		}
		readinessHandler.Enable()
	}()

	http.Handle("/images/", handlers.NewImageHandler(is, Options.AssistedServiceScheme, Options.AssistedServiceHost, Options.RequestAuthType, Options.HTTPSCAFile))
	http.Handle("/health", readinessHandler)
	http.Handle("/live", handlers.NewLivenessHandler())
	http.Handle("/metrics", promhttp.Handler())

	log.Info("Starting http handler...")
	address := fmt.Sprintf(":%s", Options.ListenPort)
	if Options.HTTPSKeyFile != "" && Options.HTTPSCertFile != "" {
		log.Fatal(http.ListenAndServeTLS(address, Options.HTTPSCertFile, Options.HTTPSKeyFile, nil))
	} else {
		log.Fatal(http.ListenAndServe(address, nil))
	}
}

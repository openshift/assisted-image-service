package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"
)

var Options struct {
	AssistedServiceScheme string `envconfig:"ASSISTED_SERVICE_SCHEME"`
	AssistedServiceHost   string `envconfig:"ASSISTED_SERVICE_HOST"`
	DataDir               string `envconfig:"DATA_DIR"`
	HTTPSKeyFile          string `envconfig:"HTTPS_KEY_FILE"`
	HTTPSCertFile         string `envconfig:"HTTPS_CERT_FILE"`
	HTTPSCAFile           string `envconfig:"HTTPS_CA_FILE"`
	ListenPort            string `envconfig:"LISTEN_PORT" default:"8080"`
	MaxConcurrentRequests int64  `envconfig:"MAX_CONCURRENT_REQUESTS" default:"400"`
	RHCOSVersions         string `envconfig:"RHCOS_VERSIONS"`
	OSImages              string `envconfig:"OS_IMAGES"`
	AllowedDomains        string `envconfig:"ALLOWED_DOMAINS"`
	InsecureSkipVerify    bool   `envconfig:"INSECURE_SKIP_VERIFY" default:"false"`
}

func main() {
	log.SetReportCaller(true)
	log.SetFormatter(&log.JSONFormatter{})
	err := envconfig.Process("cluster-image", &Options)
	if err != nil {
		log.Fatalf("Failed to process config: %v\n", err)
	}

	versionsJSON := Options.OSImages
	if versionsJSON == "" {
		versionsJSON = Options.RHCOSVersions
	}

	var versions []map[string]string
	if versionsJSON == "" {
		versions = imagestore.DefaultVersions
	} else {
		err = json.Unmarshal([]byte(versionsJSON), &versions)
		if err != nil {
			log.Fatalf("Failed to unmarshal versions: %v\n", err)
		}
	}

	is, err := imagestore.NewImageStore(isoeditor.NewEditor(Options.DataDir), Options.DataDir, Options.InsecureSkipVerify, versions)
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

	reg := prometheus.NewRegistry()
	metricsConfig := metrics.Config{
		Registry:        reg,
		Prefix:          "assisted_image_service",
		DurationBuckets: []float64{.1, 1, 10, 50, 100, 300, 600},
		SizeBuckets:     []float64{100, 1e6, 5e8, 1e9, 1e10},
	}
	mdw := middleware.New(middleware.Config{
		Recorder: metrics.NewRecorder(metricsConfig),
	})

	asc := handlers.NewAssistedServiceClient(Options.AssistedServiceScheme, Options.AssistedServiceHost, Options.HTTPSCAFile)
	imageHandler := handlers.NewImageHandler(is, asc, Options.MaxConcurrentRequests, mdw)
	if Options.AllowedDomains != "" {
		imageHandler = handlers.WithCORSMiddleware(imageHandler, Options.AllowedDomains)
	}

	var bootArtifactsHandler http.Handler = &handlers.BootArtifactsHandler{ImageStore: is}
	if Options.AllowedDomains != "" {
		bootArtifactsHandler = handlers.WithCORSMiddleware(bootArtifactsHandler, Options.AllowedDomains)
	}

	http.Handle("/images/", imageHandler)
	http.Handle("/boot-artifacts/", stdmiddleware.Handler("", mdw, bootArtifactsHandler))

	http.Handle("/health", readinessHandler)
	http.Handle("/live", handlers.NewLivenessHandler())
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	log.Info("Starting http handler...")
	address := fmt.Sprintf(":%s", Options.ListenPort)
	if Options.HTTPSKeyFile != "" && Options.HTTPSCertFile != "" {
		log.Fatal(http.ListenAndServeTLS(address, Options.HTTPSCertFile, Options.HTTPSKeyFile, nil))
	} else {
		log.Fatal(http.ListenAndServe(address, nil))
	}
}

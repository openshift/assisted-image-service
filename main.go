package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/openshift/assisted-image-service/pkg/servers"
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

	// Deprecated - use ASSISTED_SERVICE_API_TRUSTED_CA_FILE instead
	HTTPSCAFile string `envconfig:"HTTPS_CA_FILE"`

	ListenPort            string `envconfig:"LISTEN_PORT" default:"8080"`
	HTTPListenPort        string `envconfig:"HTTP_LISTEN_PORT"`
	MaxConcurrentRequests int64  `envconfig:"MAX_CONCURRENT_REQUESTS" default:"400"`
	RHCOSVersions         string `envconfig:"RHCOS_VERSIONS"`
	OSImages              string `envconfig:"OS_IMAGES"`
	AllowedDomains        string `envconfig:"ALLOWED_DOMAINS"`
	InsecureSkipVerify    bool   `envconfig:"INSECURE_SKIP_VERIFY" default:"false"`
	ImageServiceBaseURL   string `envconfig:"IMAGE_SERVICE_BASE_URL"`
	LogLevel              string `envconfig:"LOGLEVEL" default:"info"`

	// This is a path to a CA file that will be trusted when fetching OS Images
	// intended for scenarios where the OS images are served from a service that uses a custom CA
	OSImageDownloadTrustedCAFile string `envconfig:"OS_IMAGE_DOWNLOAD_TRUSTED_CA_FILE" default:""`

	// This is a path to a CA file that will be trusted for TLS connections to the Assisted Service API
	// this will be used for API calls back to the Assisted Service API
	// Will default to the value held in HTTPS_CA_FILE unless overridden

	AssistedServiceApiTrustedCAFile string `envconfig:"ASSISTED_SERVICE_API_TRUSTED_CA_FILE"`

	// OSImagesRequestHeaders contains a JSON encoded representation of any
	// HTTP headers to be sent with every request to download an OS image.
	OSImagesRequestHeaders string `envconfig:"OS_IMAGES_REQUEST_HEADERS" default:""`
	// OSImagesRequestQueryParams contains a JSON encoded representation of any
	// query parameters to be sent with every request to download an OS image.
	OSImagesRequestQueryParams string `envconfig:"OS_IMAGES_REQUEST_QUERY_PARAMS" default:""`
}

func unmarshallJSONMap(jsonMap string) (map[string]string, error) {
	result := make(map[string]string, 0)
	if jsonMap != "" {
		if err := json.Unmarshal([]byte(jsonMap), &result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func main() {
	log.SetReportCaller(true)
	log.SetFormatter(&log.JSONFormatter{})
	err := envconfig.Process("cluster-image", &Options)
	if err != nil {
		log.Fatalf("Failed to process config: %v\n", err)
	}
	if Options.AssistedServiceApiTrustedCAFile == "" {
		Options.AssistedServiceApiTrustedCAFile = Options.HTTPSCAFile
	}
	logLevel, err := log.ParseLevel(Options.LogLevel)
	if err != nil {
		log.Fatalf("unknown log level: %s", Options.LogLevel)
	}
	log.SetLevel(logLevel)

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

	osImageDownloadHeadersMap, err := unmarshallJSONMap(Options.OSImagesRequestHeaders)
	if err != nil {
		log.Fatalf("Failed to unmarshal OSImageDownloadHeaders: %v\n", err)
	}

	osImageDownloadQueryParamsMap, err := unmarshallJSONMap(Options.OSImagesRequestQueryParams)
	if err != nil {
		log.Fatalf("Failed to unmarshal OSImageDownloadQueryParams: %v\n", err)
	}

	is, err := imagestore.NewImageStore(
		isoeditor.NewEditor(Options.DataDir, isoeditor.NmstatectlPath),
		Options.DataDir,
		Options.ImageServiceBaseURL,
		Options.InsecureSkipVerify,
		versions,
		Options.OSImageDownloadTrustedCAFile,
		osImageDownloadHeadersMap,
		osImageDownloadQueryParamsMap)

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

	asc, err := handlers.NewAssistedServiceClient(Options.AssistedServiceScheme, Options.AssistedServiceHost, Options.AssistedServiceApiTrustedCAFile)
	if err != nil {
		log.Fatalf("Failed to create AssistedServiceClient: %v\n", err)
	}

	imageHandler := handlers.NewImageHandler(is, asc, Options.MaxConcurrentRequests, mdw)
	imageHandler = readinessHandler.WithMiddleware(imageHandler)
	if Options.AllowedDomains != "" {
		imageHandler = handlers.WithCORSMiddleware(imageHandler, Options.AllowedDomains)
	}

	var bootArtifactsHandler http.Handler = &handlers.BootArtifactsHandler{ImageStore: is}
	bootArtifactsHandler = readinessHandler.WithMiddleware(bootArtifactsHandler)
	if Options.AllowedDomains != "" {
		bootArtifactsHandler = handlers.WithCORSMiddleware(bootArtifactsHandler, Options.AllowedDomains)
	}

	http.Handle("/boot-artifacts/", stdmiddleware.Handler("", mdw, bootArtifactsHandler))

	http.Handle("/health", readinessHandler)
	http.Handle("/live", handlers.NewLivenessHandler())
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	// Interrupt servers on SIGINT/SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Run listen on http and https ports if HTTPSCertFile/HTTPSKeyFile set
	serverInfo := servers.New(Options.HTTPListenPort, Options.ListenPort, Options.HTTPSKeyFile, Options.HTTPSCertFile)
	if serverInfo.HasBothHandlers {
		// Make sure we filter requests when both http+https ports are open
		// Allow only pxe-initrd via HTTP in imageHandler
		imageHandler = handlers.WithInitrdViaHTTP(imageHandler)
	}
	http.Handle("/images/", imageHandler)
	http.Handle("/byapikey/", imageHandler)
	http.Handle("/byid/", imageHandler)
	http.Handle("/bytoken/", imageHandler)
	http.Handle("/s390x-initrd-addrsize", imageHandler)

	serverInfo.ListenAndServe()
	<-stop
	serverInfo.Shutdown()
}

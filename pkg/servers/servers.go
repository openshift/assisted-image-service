package servers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var tlsVersions = map[string]uint16{
	"VersionTLS10": tls.VersionTLS10,
	"VersionTLS11": tls.VersionTLS11,
	"VersionTLS12": tls.VersionTLS12,
	"VersionTLS13": tls.VersionTLS13,
}

type ServerInfo struct {
	HTTP            *http.Server
	HTTPS           *http.Server
	HTTPSKeyFile    string
	HTTPSCertFile   string
	HasBothHandlers bool
	FastShutdown    bool
}

func New(httpPort, httpsPort, HTTPSKeyFile, HTTPSCertFile string) *ServerInfo {
	servers := ServerInfo{}
	if httpsPort != "" && HTTPSKeyFile != "" && HTTPSCertFile != "" {
		// Run HTTPS listener when port, key and cert are specified
		// This is default in operator deployments
		servers.HTTPS = &http.Server{
			Addr:              fmt.Sprintf(":%s", httpsPort),
			ReadHeaderTimeout: 3 * time.Second,
		}
		servers.HTTPSCertFile = HTTPSCertFile
		servers.HTTPSKeyFile = HTTPSKeyFile
	} else if httpPort == "" {
		// Run HTTP listener on HTTPS port if httpPort is not set
		// This is default in podman deployment
		servers.HTTP = &http.Server{
			Addr:              fmt.Sprintf(":%s", httpsPort),
			ReadHeaderTimeout: 3 * time.Second,
		}
	}
	if httpPort != "" {
		// Run HTTP listener if httpPort is set
		servers.HTTP = &http.Server{
			Addr:              fmt.Sprintf(":%s", httpPort),
			ReadHeaderTimeout: 3 * time.Second,
		}
	}
	servers.HasBothHandlers = servers.HTTP != nil && servers.HTTPS != nil
	return &servers
}

func shutdown(name string, server *http.Server) {
	if err := server.Shutdown(context.TODO()); err != nil {
		log.Infof("%s shutdown failed: %v", name, err)
		if err := server.Close(); err != nil {
			log.Fatalf("%s emergency shutdown failed: %v", name, err)
		}
	} else {
		log.Infof("%s server terminated gracefully", name)
	}
}

func (s *ServerInfo) ListenAndServe() {
	if s.HTTP != nil {
		go s.httpListen()
	}

	if s.HTTPS != nil {
		go s.httpsListen()
	}
}

func (s *ServerInfo) Shutdown() bool {
	if s.HTTPS != nil {
		if s.FastShutdown {
			s.HTTPS.Close()
		} else {
			shutdown("HTTPS", s.HTTPS)
		}
	}
	if s.HTTP != nil {
		if s.FastShutdown {
			s.HTTP.Close()
		} else {
			shutdown("HTTP", s.HTTP)
		}
	}
	return true
}

// ApplyTLSProfile configures the HTTPS server with the given TLS minimum
// version and cipher suites. The version should be a Go TLS version name
// (e.g., "VersionTLS12"). Cipher suites should be comma-separated IANA names.
// If minVersion is empty, no TLS configuration is applied.
func (s *ServerInfo) ApplyTLSProfile(minVersion, cipherSuites string) {
	if s.HTTPS == nil || minVersion == "" {
		return
	}

	version, ok := tlsVersions[minVersion]
	if !ok {
		log.Warnf("Unknown TLS version %q, skipping TLS profile configuration", minVersion)
		return
	}

	cfg := &tls.Config{MinVersion: version}

	if version < tls.VersionTLS13 && cipherSuites != "" {
		var unsupported []string
		for _, name := range strings.Split(cipherSuites, ",") {
			name = strings.TrimSpace(name)
			if id, ok := cipherSuiteID(name); ok {
				cfg.CipherSuites = append(cfg.CipherSuites, id)
			} else {
				unsupported = append(unsupported, name)
			}
		}
		if len(unsupported) > 0 {
			log.Infof("TLS profile contains cipher suites that Go does not implement, ignoring: %s", strings.Join(unsupported, ", "))
		}
	}

	s.HTTPS.TLSConfig = cfg
	log.Infof("TLS profile applied: MinVersion=%s", minVersion)
}

// cipherSuiteID resolves an IANA cipher suite name to its Go identifier. Both
// the secure and the insecure lists are searched, since the Old TLS profile
// legitimately includes suites that Go classifies as insecure and the cluster
// profile is the authority on what is allowed.
func cipherSuiteID(name string) (uint16, bool) {
	for _, suite := range tls.CipherSuites() {
		if suite.Name == name {
			return suite.ID, true
		}
	}
	for _, suite := range tls.InsecureCipherSuites() {
		if suite.Name == name {
			return suite.ID, true
		}
	}
	return 0, false
}

func (s *ServerInfo) httpListen() {
	log.Infof("Starting http handler on %s...", s.HTTP.Addr)
	if err := s.HTTP.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP listener closed: %v", err)
	}
}

func (s *ServerInfo) httpsListen() {
	log.Infof("Starting https handler on %s...", s.HTTPS.Addr)
	if err := s.HTTPS.ListenAndServeTLS(s.HTTPSCertFile, s.HTTPSKeyFile); err != http.ErrServerClosed {
		log.Fatalf("HTTPS listener closed: %v", err)
	}
}

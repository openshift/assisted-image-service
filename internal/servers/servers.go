package servers

import (
	"context"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"
)

type ServerInfo struct {
	HTTP          http.Server
	HTTPS         http.Server
	HTTPSKeyFile  string
	HTTPSCertFile string
}

func Init(httpPort, httpsPort, HTTPSKeyFile, HTTPSCertFile string) *ServerInfo {
	servers := ServerInfo{}
	if httpsPort != "" && HTTPSKeyFile != "" && HTTPSCertFile != "" {
		servers.HTTPS = http.Server{
			Addr: fmt.Sprintf(":%s", httpsPort),
		}
		servers.HTTPSCertFile = HTTPSCertFile
		servers.HTTPSKeyFile = HTTPSKeyFile
		go servers.httpsListen()
	}
	if httpPort != "" {
		servers.HTTP = http.Server{
			Addr: fmt.Sprintf(":%s", httpPort),
		}
		go servers.httpListen()
	}
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

func (s *ServerInfo) Shutdown() bool {
	if s.HTTPSKeyFile != "" && s.HTTPSCertFile != "" {
		shutdown("HTTPS", &s.HTTPS)
	}
	shutdown("HTTP", &s.HTTP)
	return true
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

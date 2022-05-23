package servers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var tmpDir string
var httpsKeyFile, httpsCertFile *os.File
var ready func(rw http.ResponseWriter, req *http.Request)
var mux *http.ServeMux
var httpClient, httpsClient *http.Client

var _ = BeforeSuite(func() {
	var err error
	// Generate self-signed key and cert
	tmpDir, err = ioutil.TempDir("", "")
	Expect(err).NotTo(HaveOccurred())
	httpsKeyFile, err = ioutil.TempFile(tmpDir, "https.key")
	Expect(err).NotTo(HaveOccurred())
	httpsCertFile, err = ioutil.TempFile(tmpDir, "https.crt")
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		IsCA:                  true,
		BasicConstraintsValid: true,
		SubjectKeyId:          []byte{1, 2, 3},
		SerialNumber:          big.NewInt(1234),
		Subject: pkix.Name{
			Country:      []string{"Earth"},
			Organization: []string{"Yes"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(5, 5, 5),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageCertSign,
	}

	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	var pemkey = &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privatekey)}
	err = pem.Encode(httpsKeyFile, pemkey)
	Expect(err).NotTo(HaveOccurred())

	publickey := &privatekey.PublicKey
	var parent = template
	cert, err := x509.CreateCertificate(rand.Reader, template, parent, publickey, privatekey)
	Expect(err).NotTo(HaveOccurred())
	var pemcert = &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert}
	err = pem.Encode(httpsCertFile, pemcert)
	Expect(err).NotTo(HaveOccurred())

	ready = func(rw http.ResponseWriter, req *http.Request) {
		_, _ = rw.Write([]byte("hello"))
	}
	mux = http.NewServeMux()
	mux.Handle("/ready", http.HandlerFunc(ready))

	httpClient = &http.Client{Transport: &http.Transport{}}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	httpsClient = &http.Client{Transport: tr}
})

var _ = AfterSuite(func() {
	Expect(os.RemoveAll(tmpDir)).To(Succeed())
})

var _ = Describe("HTTPListeners", func() {
	It("starts http only server", func() {
		listeners := New("8080", "", "", "")

		Expect(listeners.HTTP).NotTo(BeNil())
		Expect(listeners.HTTP.Addr).To(Equal(":8080"))
		Expect(listeners.HTTPS).To(BeNil())
		Expect(listeners.HasBothHandlers).To(BeFalse())

		listeners.ListenAndServe()
		timeout := time.Second
		_, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", "8080"), timeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts http only server - no certs", func() {
		listeners := New("8081", "8443", "", "")

		Expect(listeners.HTTP).NotTo(BeNil())
		Expect(listeners.HTTP.Addr).To(Equal(":8081"))
		Expect(listeners.HTTPS).To(BeNil())
		Expect(listeners.HasBothHandlers).To(BeFalse())

		listeners.ListenAndServe()

		timeout := time.Second
		_, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", "8081"), timeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts https only server", func() {
		listeners := New("", "8443", httpsKeyFile.Name(), httpsCertFile.Name())

		Expect(listeners.HTTP).To(BeNil())
		Expect(listeners.HTTPS).NotTo(BeNil())
		Expect(listeners.HTTPS.Addr).To(Equal(":8443"))
		Expect(listeners.HasBothHandlers).To(BeFalse())

		listeners.ListenAndServe()

		timeout := time.Second
		_, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", "8443"), timeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts both servers", func() {
		listeners := New("8082", "8444", httpsKeyFile.Name(), httpsCertFile.Name())

		Expect(listeners.HTTP).NotTo(BeNil())
		Expect(listeners.HTTP.Addr).To(Equal(":8082"))
		Expect(listeners.HTTPS).NotTo(BeNil())
		Expect(listeners.HTTPS.Addr).To(Equal(":8444"))
		Expect(listeners.HasBothHandlers).To(BeTrue())

		listeners.ListenAndServe()

		timeout := time.Second
		_, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", "8082"), timeout)
		Expect(err).NotTo(HaveOccurred())
		_, err = net.DialTimeout("tcp", net.JoinHostPort("localhost", "8444"), timeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts http server on https port with no certs", func() {
		listeners := New("", "8445", "", "")

		Expect(listeners.HTTP).NotTo(BeNil())
		Expect(listeners.HTTP.Addr).To(Equal(":8445"))
		Expect(listeners.HTTPS).To(BeNil())
		Expect(listeners.HasBothHandlers).To(BeFalse())

		listeners.ListenAndServe()

		timeout := time.Second
		_, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", "8445"), timeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts http server with custom handler", func() {
		listeners := New("", "8446", "", "")

		Expect(listeners.HTTP).NotTo(BeNil())
		Expect(listeners.HTTP.Addr).To(Equal(":8446"))
		Expect(listeners.HTTPS).To(BeNil())
		Expect(listeners.HasBothHandlers).To(BeFalse())

		listeners.HTTP.Handler = mux
		listeners.ListenAndServe()

		req, err := http.NewRequest(http.MethodGet, "http://localhost:8446/ready", nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err := httpClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts https server with custom handler", func() {
		listeners := New("", "8447", httpsKeyFile.Name(), httpsCertFile.Name())

		Expect(listeners.HTTP).To(BeNil())
		Expect(listeners.HTTPS).NotTo(BeNil())
		Expect(listeners.HTTPS.Addr).To(Equal(":8447"))
		Expect(listeners.HasBothHandlers).To(BeFalse())

		listeners.HTTPS.Handler = mux
		listeners.ListenAndServe()

		req, err := http.NewRequest(http.MethodGet, "https://localhost:8447/ready", nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err := httpsClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		Expect(listeners.Shutdown()).To(BeTrue())
	})

	It("starts both servers with custom handler", func() {
		listeners := New("8088", "8448", httpsKeyFile.Name(), httpsCertFile.Name())

		Expect(listeners.HTTP).NotTo(BeNil())
		Expect(listeners.HTTP.Addr).To(Equal(":8088"))
		Expect(listeners.HTTPS).NotTo(BeNil())
		Expect(listeners.HTTPS.Addr).To(Equal(":8448"))
		Expect(listeners.HasBothHandlers).To(BeTrue())

		listeners.HTTP.Handler = mux
		listeners.HTTPS.Handler = mux
		listeners.ListenAndServe()

		req, err := http.NewRequest(http.MethodGet, "http://localhost:8088/ready", nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err := httpClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		req, err = http.NewRequest(http.MethodGet, "https://localhost:8448/ready", nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err = httpsClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		Expect(listeners.Shutdown()).To(BeTrue())
	})
})

func TestServers(t *testing.T) {
	RegisterFailHandler(Fail)
	log.SetOutput(io.Discard)
	RunSpecs(t, "servers")
}

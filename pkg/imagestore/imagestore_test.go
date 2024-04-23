package imagestore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

var (
	imageServiceBaseURL = "http://images.example.com"
	rootfsURL           = "http://images.example.com/boot-artifacts/rootfs?arch=x86_64&version=%s"
)

func TestImageStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "imagestore")
}

var _ = Context("with a data directory configured", func() {
	var (
		dataDir string
	)

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "imageStoreTest")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
	})

	Context("with an HTTPS server", func() {

		var (
			ts                            *ghttp.Server
			ctx                           = context.Background()
			caCertDir                     string
			privateKeyDir                 string
			caCertFileName                string
			osImageDownloadHeadersMap     map[string]string
			osImageDownloadQueryParamsMap map[string]string
		)

		var setupTLSForTest = func() (*tls.Config, string) {
			var err error
			caCertDir, err = os.MkdirTemp("", "")
			Expect(err).NotTo(HaveOccurred())
			privateKeyDir, err = os.MkdirTemp("", "")
			Expect(err).NotTo(HaveOccurred())
			privateKeyFile, err := os.CreateTemp(privateKeyDir, "private.key")
			Expect(err).NotTo(HaveOccurred())
			caCertFile, err := os.CreateTemp(caCertDir, "ca.cert")
			Expect(err).NotTo(HaveOccurred())
			privateKeyBuffer := new(bytes.Buffer)
			caCertBuffer := new(bytes.Buffer)
			caCertTemplate := &x509.Certificate{
				IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
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
			err = pem.Encode(privateKeyFile, pemkey)
			Expect(err).NotTo(HaveOccurred())
			err = pem.Encode(privateKeyBuffer, pemkey)
			Expect(err).NotTo(HaveOccurred())
			publickey := &privatekey.PublicKey
			var parent = caCertTemplate
			caCert, err := x509.CreateCertificate(rand.Reader, caCertTemplate, parent, publickey, privatekey)
			Expect(err).NotTo(HaveOccurred())
			var pemcert = &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: caCert}
			err = pem.Encode(caCertFile, pemcert)
			Expect(err).NotTo(HaveOccurred())
			err = pem.Encode(caCertBuffer, pemcert)
			Expect(err).NotTo(HaveOccurred())
			Expect(err).NotTo(HaveOccurred())
			serverCert, err := tls.X509KeyPair(caCertBuffer.Bytes(), privateKeyBuffer.Bytes())
			Expect(err).NotTo(HaveOccurred())
			return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverCert}}, caCertFile.Name()
		}

		BeforeEach(func() {
			var tlsConfig *tls.Config
			tlsConfig, caCertFileName = setupTLSForTest()
			ts = ghttp.NewUnstartedServer()
			ts.HTTPTestServer.TLS = tlsConfig
			ts.HTTPTestServer.StartTLS()
		})

		AfterEach(func() {
			ts.Close()
			Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
		})

		Describe("Populate", func() {
			var (
				ctrl          *gomock.Controller
				mockEditor    *isoeditor.MockEditor
				validVolumeID = "rhcos-411.86.202210041459-0"
				version       = map[string]string{
					"openshift_version": "4.8",
					"cpu_architecture":  "x86_64",
					"version":           "48.84.202109241901-0",
				}
			)

			BeforeEach(func() {
				ctrl = gomock.NewController(GinkgoT())
				mockEditor = isoeditor.NewMockEditor(ctrl)
				osImageDownloadHeadersMap = map[string]string{}
				osImageDownloadQueryParamsMap = map[string]string{}
			})

			isoInfo := func(id string) ([]byte, http.Header) {
				content := make([]byte, 32840)
				copy(content[32808:], id)
				header := http.Header{}
				header.Add("Content-Length", strconv.Itoa(len(content)))

				return content, header
			}

			It("downloads an image correctly", func() {
				isoContent, isoHeader := isoInfo(validVolumeID)
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, caCertFileName, osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal(isoContent))
			})

		})
	})

	Context("with a test server", func() {
		var (
			ts                            *ghttp.Server
			ctx                           = context.Background()
			osImageDownloadHeadersMap     map[string]string
			osImageDownloadQueryParamsMap map[string]string
		)

		BeforeEach(func() {
			ts = ghttp.NewServer()
			osImageDownloadHeadersMap = map[string]string{}
			osImageDownloadQueryParamsMap = map[string]string{}
		})

		AfterEach(func() {
			ts.Close()
			Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
		})

		Describe("Populate", func() {
			var (
				ctrl          *gomock.Controller
				mockEditor    *isoeditor.MockEditor
				validVolumeID = "rhcos-411.86.202210041459-0"
				version       = map[string]string{
					"openshift_version": "4.8",
					"cpu_architecture":  "x86_64",
					"version":           "48.84.202109241901-0",
				}
				versionPatch = map[string]string{
					"openshift_version": "4.8.1",
					"cpu_architecture":  "x86_64",
					"version":           "48.84.202109241901-0",
				}
			)

			BeforeEach(func() {
				ctrl = gomock.NewController(GinkgoT())
				mockEditor = isoeditor.NewMockEditor(ctrl)
			})

			isoInfo := func(id string) ([]byte, http.Header) {
				content := make([]byte, 32840)
				copy(content[32808:], id)
				header := http.Header{}
				header.Add("Content-Length", strconv.Itoa(len(content)))

				return content, header
			}

			It("passes query parameters in request when parameters have been provided during creation", func() {
				osImageDownloadQueryParamsMap["foo"] = "bar"
				osImageDownloadQueryParamsMap["bar"] = "foo"
				isoContent, isoHeader := isoInfo(validVolumeID)
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso", "foo=bar&bar=foo"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal(isoContent))
			})

			It("passes http headers in request when parameters have been provided during creation", func() {
				osImageDownloadHeadersMap["foo"] = "bar"
				osImageDownloadHeadersMap["bar"] = "foo"
				isoContent, isoHeader := isoInfo(validVolumeID)
				httpHeader := http.Header{}
				httpHeader.Add("foo", "bar")
				httpHeader.Add("bar", "foo")
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyHeader(httpHeader),
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal(isoContent))
			})

			It("downloads an image correctly", func() {
				isoContent, isoHeader := isoInfo(validVolumeID)
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal(isoContent))
			})

			It("fails when the download fails", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/fail.iso"),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				version["url"] = ts.URL() + "/fail.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("fails and removes the file when the downloaded iso has an invalid volume ID", func() {
				isoContent, isoHeader := isoInfo("Fedora-S-dvd-x86_64-37")
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				Expect(is.Populate(ctx)).NotTo(Succeed())

				_, err = os.Stat(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"))
				Expect(err).To(MatchError(fs.ErrNotExist))
			})

			It("fails when minimal iso creation fails", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, "someisocontenthere"),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(fmt.Errorf("minimal iso creation failed"))
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("doesn't download if the file already exists", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/dontcallthis.iso"),
						http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Fail("endpoint should not be queried") }),
					),
				)
				version["url"] = ts.URL() + "/dontcallthis.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				Expect(os.WriteFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"), []byte("moreisocontent"), 0600)).To(Succeed())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("recreates the minimal iso even when it's already present", func() {
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				fullPath := filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso")
				Expect(os.WriteFile(fullPath, []byte("moreisocontent"), 0600)).To(Succeed())

				minimalPath := filepath.Join(dataDir, "rhcos-minimal-iso-4.8-48.84.202109241901-0-x86_64.iso")
				Expect(os.WriteFile(minimalPath, []byte("minimalisocontent"), 0600)).To(Succeed())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(fullPath, rootfs, "x86_64", minimalPath).Return(nil)

				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("downloads image with x.y.z openshift_version correctly", func() {
				isoContent, isoHeader := isoInfo(validVolumeID)
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/somepatchversion.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				versionPatch["url"] = ts.URL() + "/somepatchversion.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{versionPatch}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, versionPatch["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "rhcos-full-iso-4.8.1-48.84.202109241901-0-x86_64.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal(isoContent))
			})

			It("populates Fedora/Centos images correctly", func() {
				validVolumeIDs := []string{"fedora-coreos-35.20220101.0.3", "scos-413.9.20231000101-0"}
				for _, testValidVolumeID := range validVolumeIDs {
					isoContent, isoHeader := isoInfo(testValidVolumeID)
					ts.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", "/somepatchversion.iso"),
							ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
						),
					)
					versionPatch["url"] = ts.URL() + "/somepatchversion.iso"
					is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{versionPatch}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
					Expect(err).NotTo(HaveOccurred())

					rootfs := fmt.Sprintf(rootfsURL, versionPatch["openshift_version"])
					mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
					Expect(is.Populate(ctx)).To(Succeed())
				}
			})

			It("cleans up files that are not configured isos", func() {
				oldISOPath := filepath.Join(dataDir, "rhcos-full-iso-4.7-47.84.202109241831-0-x86_64.iso")
				Expect(os.WriteFile(oldISOPath, []byte("oldisocontent"), 0600)).To(Succeed())

				isoContent, isoHeader := isoInfo(validVolumeID)
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				rootfs := fmt.Sprintf(rootfsURL, version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				_, err = os.Stat(oldISOPath)
				Expect(err).To(MatchError(fs.ErrNotExist))
			})

			It("cleans up corrupted downloads", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, "someisocontenthere", http.Header{"Content-Length": []string{"1"}}),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, imageServiceBaseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				err = is.Populate(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unexpected EOF"))

				_, err = os.Stat(filepath.Join(dataDir, "rhcos-full-iso-4.8-48.84.202109241901-0-x86_64.iso"))
				Expect(err).To(MatchError(fs.ErrNotExist))
			})

			It("fails when imageServiceBaseURL is not set", func() {
				is, err := NewImageStore(mockEditor, dataDir, "", false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "", "x86_64", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("downloads fails when imageServiceBaseURL is invalid", func() {
				isoContent, isoHeader := isoInfo(validVolumeID)
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, isoContent, isoHeader),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				baseURL := ":"
				is, err := NewImageStore(mockEditor, dataDir, baseURL, false, []map[string]string{version}, "", osImageDownloadHeadersMap, osImageDownloadQueryParamsMap)
				Expect(err).ToNot(HaveOccurred())

				rootfs := fmt.Sprintf("https://images.example.com/api/assisted-images/boot-artifacts/rootfs?arch=x86_64&version=%s", version["openshift_version"])
				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), rootfs, "x86_64", gomock.Any()).Return(nil)
				err = is.Populate(ctx)
				Expect(err).ToNot(Succeed())
				Expect(err.Error()).To(Equal("failed to build rootfs URL: parse \":\": missing protocol scheme"))
			})
		})
	})
})

var _ = Describe("PathForParams", func() {
	It("creates the correct path", func() {
		versions := []map[string]string{{
			"openshift_version": "4.8",
			"cpu_architecture":  "x86_64",
			"url":               "http://example.com/image/x86_64-48.iso",
			"version":           "48.84.202109241901-0",
		}}
		is, err := NewImageStore(nil, "/tmp/some/dir", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).NotTo(HaveOccurred())
		expected := "/tmp/some/dir/rhcos-full-4.8-48.84.202109241901-0-x86_64.iso"
		Expect(is.PathForParams("full", "4.8", "x86_64")).To(Equal(expected))
	})
})

var _ = Describe("HaveVersion", func() {
	var (
		versions = []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"url":               "http://example.com/image/x86_64-48.iso",
				"version":           "48.84.202109241901-0",
			},
			{
				"openshift_version": "4.9",
				"cpu_architecture":  "arm64",
				"url":               "http://example.com/image/arm64-49.iso",
				"version":           "49.84.202110081407-0",
			},
			{
				"openshift_version": "4.15",
				"cpu_architecture":  "s390x",
				"url":               "http://example.com/image/s390x-415.iso",
				"version":           "415.92.202403212258-0",
			},
		}
		store ImageStore
	)

	BeforeEach(func() {
		var err error
		store, err = NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
	})

	It("is true for versions that are present", func() {
		Expect(store.HaveVersion("4.8", "x86_64")).To(BeTrue())
		Expect(store.HaveVersion("4.9", "arm64")).To(BeTrue())
		Expect(store.HaveVersion("4.15", "s390x")).To(BeTrue())
	})

	It("is false for versions that are missing", func() {
		Expect(store.HaveVersion("4.9", "x86_64")).To(BeFalse())
		Expect(store.HaveVersion("4.8", "arm64")).To(BeFalse())
		Expect(store.HaveVersion("4.7", "x86_64")).To(BeFalse())
		Expect(store.HaveVersion("4.8", "aarch64")).To(BeFalse())
		Expect(store.HaveVersion("4.11", "s390x")).To(BeFalse())
	})
})

var _ = Describe("NewImageStore", func() {
	It("should not error with valid version", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"url":               "http://example.com/image/x86_64-48.iso",
				"version":           "48.84.202109241901-0",
			},
		}
		_, err := NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should error when RHCOS_IMAGES are not set i.e. versions is an empty slice", func() {
		versions := []map[string]string{}
		_, err := NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("invalid versions: must not be empty"))

	})

	It("should error when openshift_version is not set", func() {
		versions := []map[string]string{
			{
				"cpu_architecture": "x86_64",
				"url":              "http://example.com/image/x86_64-48.iso",
				"version":          "48.84.202109241901-0",
			},
		}
		_, err := NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).To(HaveOccurred())
	})

	It("should error when cpu_architecture is not set", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"url":               "http://example.com/image/x86_64-48.iso",
				"version":           "48.84.202109241901-0",
			},
		}
		_, err := NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).To(HaveOccurred())
	})

	It("should error when url is not set", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"version":           "48.84.202109241901-0",
			},
		}
		_, err := NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).To(HaveOccurred())
	})

	It("should error when version is not set", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"url":               "http://example.com/image/x86_64-48.iso",
			},
		}
		_, err := NewImageStore(nil, "", imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
		Expect(err).To(HaveOccurred())
	})
})

package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cavaliercoder/go-cpio"
	diskfs "github.com/diskfs/go-diskfs"
	"github.com/google/uuid"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/slok/go-http-metrics/middleware"
)

var (
	versions = []map[string]string{
		{
			"openshift_version": "pre-release",
			"cpu_architecture":  "arm64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/arm64/dependencies/rhcos/pre-release/latest/rhcos-live.aarch64.iso",
			"version":           "arm-latest",
		},
		{
			"openshift_version": "4.8",
			"cpu_architecture":  "x86_64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/latest/rhcos-live.x86_64.iso",
			"version":           "4.8-latest",
		},
		{
			"openshift_version": "pre-release",
			"cpu_architecture":  "x86_64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/4.10.0-rc.0/rhcos-live.x86_64.iso",
			"version":           "x86_64-latest",
		},
		{
			"openshift_version": "fcos-pre-release",
			"cpu_architecture":  "x86_64",
			"url":               "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/x86_64/fedora-coreos-35.20220103.3.0-live.x86_64.iso",
			"version":           "x86_64-latest",
		},
		{
			"openshift_version": "fcos-pre-release",
			"cpu_architecture":  "arm64",
			"url":               "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/aarch64/fedora-coreos-35.20220103.3.0-live.aarch64.iso",
			"version":           "arm-latest",
		},
		{
			"openshift_version": "4.11",
			"cpu_architecture":  "s390x",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/s390x/dependencies/rhcos/4.11/4.11.9/rhcos-4.11.9-s390x-live.s390x.iso",
			"version":           "s390x-latest",
		},
		{
			"openshift_version": "4.11",
			"cpu_architecture":  "ppc64le",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/ppc64le/dependencies/rhcos/4.11/4.11.9/rhcos-4.11.9-ppc64le-live.ppc64le.iso",
			"version":           "ppc64le-latest",
		},
		{
			"openshift_version": "scos-prerelease",
			"cpu_architecture":  "x86_64",
			"url":               "https://okd-scos.s3.amazonaws.com/okd-scos/builds/413.9.202302280609-0/x86_64/scos-413.9.202302280609-0-live.x86_64.iso",
			"version":           "x86_64-latest",
		},
	}

	imageDir            string
	imageStore          imagestore.ImageStore
	imageServiceBaseURL = "http://images.example.com"
)

var _ = Describe("Image integration tests", func() {
	var (
		isoFilename    string
		imageID        string
		assistedServer *ghttp.Server
		imageServer    *httptest.Server
		imageClient    *http.Client
	)

	buildInfraenvResponse := func(args ...string) []byte {
		if len(args) == 0 {
			return []byte("{}")
		}
		var infraEnv struct {
			// JSON formatted string array representing the discovery image kernel arguments.
			KernelArguments *string `json:"kernel_arguments,omitempty"`
		}
		kargs, err := isoeditor.KargsToStr(args)
		Expect(err).ToNot(HaveOccurred())
		infraEnv.KernelArguments = &kargs
		b, err := json.Marshal(&infraEnv)
		Expect(err).ToNot(HaveOccurred())
		return b
	}

	testcases := []struct {
		name               string
		fileName           string
		imageType          string
		expectedIgnition   []byte
		expectedRamdisk    []byte
		expectedExtraKargs []string
	}{
		{
			name:             "full-iso",
			imageType:        imagestore.ImageTypeFull,
			fileName:         "full.iso",
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  nil,
		},
		{
			name:               "full-iso-with-kargs",
			imageType:          imagestore.ImageTypeFull,
			fileName:           "full.iso",
			expectedIgnition:   []byte("someignitioncontent"),
			expectedRamdisk:    nil,
			expectedExtraKargs: []string{"p1", "p1", `key=value`},
		},
		{
			name:             "minimal-iso-with-initrd",
			imageType:        imagestore.ImageTypeMinimal,
			fileName:         "minimal.iso",
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  []byte("someramdiskcontent"),
		},
		{
			name:             "minimal-iso-without-initrd",
			imageType:        imagestore.ImageTypeMinimal,
			fileName:         "minimal.iso",
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  []byte(""),
		},
		{
			name:               "minimal-iso-without-initrd-with-kargs",
			imageType:          imagestore.ImageTypeMinimal,
			fileName:           "minimal.iso",
			expectedIgnition:   []byte("someignitioncontent"),
			expectedRamdisk:    []byte(""),
			expectedExtraKargs: []string{"p5", "p6", `key=value`},
		},
	}

	for i := range testcases {
		tc := testcases[i]

		Context(tc.name, func() {
			BeforeEach(func() {
				imageID = uuid.New().String()

				// Set up assisted service
				assistedServer = ghttp.NewServer()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())
				queryString := fmt.Sprintf("file_name=discovery.ign&discovery_iso_type=%s", tc.imageType)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), queryString),
						ghttp.RespondWith(http.StatusOK, tc.expectedIgnition),
					),
				)
				if tc.expectedRamdisk != nil {
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusOK, tc.expectedRamdisk),
						),
					)
				}
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s", imageID)),
						ghttp.RespondWith(http.StatusOK, buildInfraenvResponse(tc.expectedExtraKargs...)),
					),
				)

				asc, err := handlers.NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				mdw := middleware.New(middleware.Config{})
				imageServer = httptest.NewServer(handlers.NewImageHandler(imageStore, asc, 1, mdw))
				imageClient = imageServer.Client()
			})

			AfterEach(func() {
				assistedServer.Close()
				imageServer.Close()
				Expect(os.Remove(isoFilename)).To(Succeed())
			})

			for i := range versions {
				version := versions[i]

				It("returns a properly generated "+tc.name+" iso image "+version["version"], func() {
					if version["cpu_architecture"] == "s390x" {
						if tc.imageType == imagestore.ImageTypeMinimal {
							Skip("minimal ISO is not supported for s390x architecture")
						}
						if tc.expectedExtraKargs != nil {
							Skip("Karg editing is not supported for s390x architecture")
						}
					}

					By("getting an iso")
					path := fmt.Sprintf("/byid/%s/%s/%s/%s", imageID, version["openshift_version"], version["cpu_architecture"], tc.fileName)
					resp, err := imageClient.Get(imageServer.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					isoFile, err := os.CreateTemp("", fmt.Sprintf("imageTest-%s-%s.%s.iso", version["openshift_version"], tc.name, version["cpu_architecture"]))
					Expect(err).NotTo(HaveOccurred())
					_, err = io.Copy(isoFile, resp.Body)
					Expect(err).NotTo(HaveOccurred())
					isoFilename = isoFile.Name()

					By("opening the iso")
					d, err := diskfs.Open(isoFilename, diskfs.WithOpenMode(diskfs.ReadOnly))
					Expect(err).NotTo(HaveOccurred())
					fs, err := d.GetFilesystem(0)
					Expect(err).NotTo(HaveOccurred())

					By("verifying ignition content")
					f, err := fs.OpenFile("/images/ignition.img", os.O_RDONLY)
					Expect(err).NotTo(HaveOccurred())
					gzipReader, err := gzip.NewReader(f)
					Expect(err).NotTo(HaveOccurred())
					cpioReader := cpio.NewReader(gzipReader)
					hdr, err := cpioReader.Next()
					Expect(err).NotTo(HaveOccurred())
					Expect(hdr.Name).To(Equal("config.ign"))
					Expect(hdr.Size).To(Equal(int64(len(tc.expectedIgnition))))
					content, err := io.ReadAll(cpioReader)
					Expect(err).NotTo(HaveOccurred())
					Expect(content).To(Equal(tc.expectedIgnition))

					if tc.expectedRamdisk != nil {
						By("verifying ramdisk content")
						f, err := fs.OpenFile("/images/assisted_installer_custom.img", os.O_RDONLY)
						Expect(err).NotTo(HaveOccurred())

						content, err := io.ReadAll(f)
						Expect(err).NotTo(HaveOccurred())
						Expect(bytes.TrimRight(content, "\x00")).To(Equal(tc.expectedRamdisk))
					}
					if len(tc.expectedExtraKargs) > 0 {
						By("verifying kernel arguments content")
						files, err := isoeditor.KargsFiles(isoFilename)
						Expect(err).ToNot(HaveOccurred())
						for _, fname := range files {
							f, err := fs.OpenFile(fname, os.O_RDONLY)
							Expect(err).ToNot(HaveOccurred())
							b, err := io.ReadAll(f)
							Expect(err).NotTo(HaveOccurred())
							Expect(string(b)).To(MatchRegexp(" " + strings.Join(tc.expectedExtraKargs, " ") + "\n#+ COREOS_KARG_EMBED_AREA"))
						}
					}
				})
			}
		})
	}
})

var _ = BeforeSuite(func() {
	var err error

	imageDir, err = os.MkdirTemp("", "imagesTest")
	Expect(err).To(BeNil())

	nmstatectl, err := os.CreateTemp(os.TempDir(), "nmstatectl")
	Expect(err).ToNot(HaveOccurred())

	imageStore, err = imagestore.NewImageStore(isoeditor.NewEditor(imageDir, nmstatectl.Name()), imageDir, imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{})
	Expect(err).NotTo(HaveOccurred())

	err = imageStore.Populate(context.Background())
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	Expect(os.RemoveAll(imageDir)).To(Succeed())
})

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration testing in short mode")
		return
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "image building tests")
}

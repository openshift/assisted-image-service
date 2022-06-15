package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
			"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/arm64/dependencies/rhcos/pre-release/latest/rhcos-live-rootfs.aarch64.img",
			"version":           "arm-latest",
		},
		{
			"openshift_version": "4.8",
			"cpu_architecture":  "x86_64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/latest/rhcos-live.x86_64.iso",
			"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/latest/rhcos-live-rootfs.x86_64.img",
			"version":           "4.8-latest",
		},
		{
			"openshift_version": "pre-release",
			"cpu_architecture":  "x86_64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/4.10.0-rc.0/rhcos-live.x86_64.iso",
			"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/4.10.0-rc.0/rhcos-live-rootfs.x86_64.img",
			"version":           "x86_64-latest",
		},
		{
			"openshift_version": "fcos-pre-release",
			"cpu_architecture":  "x86_64",
			"url":               "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/x86_64/fedora-coreos-35.20220103.3.0-live.x86_64.iso",
			"rootfs_url":        "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/x86_64/fedora-coreos-35.20220103.3.0-live-rootfs.x86_64.img",
			"version":           "x86_64-latest",
		},
		{
			"openshift_version": "fcos-pre-release",
			"cpu_architecture":  "arm64",
			"url":               "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/aarch64/fedora-coreos-35.20220103.3.0-live.aarch64.iso",
			"rootfs_url":        "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/aarch64/fedora-coreos-35.20220103.3.0-live-initramfs.aarch64.imgg",
			"version":           "arm-latest",
		},
	}

	imageDir           string
	imageStore         imagestore.ImageStore
	imageServiceScheme = "http"
	imageServiceHost   = "images.example.com"
)

var _ = Describe("Image integration tests", func() {
	var (
		isoFilename    string
		imageID        string
		assistedServer *ghttp.Server
		imageServer    *httptest.Server
		imageClient    *http.Client
	)

	testcases := []struct {
		name             string
		imageType        string
		expectedIgnition []byte
		expectedRamdisk  []byte
	}{
		{
			name:             "full-iso",
			imageType:        imagestore.ImageTypeFull,
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  nil,
		},
		{
			name:             "minimal-iso-with-initrd",
			imageType:        imagestore.ImageTypeMinimal,
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  []byte("someramdiskcontent"),
		},
		{
			name:             "minimal-iso-without-initrd",
			imageType:        imagestore.ImageTypeMinimal,
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  []byte(""),
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
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), "file_name=discovery.ign"),
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

				It("returns a properly generated "+tc.name+" iso image", func() {
					By("getting an iso")
					path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageID, version["openshift_version"], tc.imageType, version["cpu_architecture"])
					resp, err := imageClient.Get(imageServer.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					isoFile, err := ioutil.TempFile("", fmt.Sprintf("imageTest-%s-%s.%s.iso", version["openshift_version"], tc.name, version["cpu_architecture"]))
					Expect(err).NotTo(HaveOccurred())
					_, err = io.Copy(isoFile, resp.Body)
					Expect(err).NotTo(HaveOccurred())
					isoFilename = isoFile.Name()

					By("opening the iso")
					d, err := diskfs.OpenWithMode(isoFilename, diskfs.ReadOnly)
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
					content, err := ioutil.ReadAll(cpioReader)
					Expect(err).NotTo(HaveOccurred())
					Expect(content).To(Equal(tc.expectedIgnition))

					if tc.expectedRamdisk != nil {
						By("verifying ramdisk content")
						f, err := fs.OpenFile("/images/assisted_installer_custom.img", os.O_RDONLY)
						Expect(err).NotTo(HaveOccurred())

						content, err := ioutil.ReadAll(f)
						Expect(err).NotTo(HaveOccurred())
						Expect(bytes.TrimRight(content, "\x00")).To(Equal(tc.expectedRamdisk))
					}
				})
			}
		})
	}
})

var _ = BeforeSuite(func() {
	var err error

	imageDir, err = ioutil.TempDir("", "imagesTest")
	Expect(err).To(BeNil())

	imageStore, err = imagestore.NewImageStore(isoeditor.NewEditor(imageDir), imageDir, imageServiceScheme, imageServiceHost, false, versions)
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

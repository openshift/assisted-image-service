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
	// "path/filepath"
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
	"github.com/prometheus/client_golang/prometheus"
)

var (
	versions = []map[string]string{
		{
			"openshift_version": "pre-release",
			"cpu_architecture":  "arm64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/arm64/dependencies/rhcos/pre-release/latest/rhcos-live.aarch64.iso",
			"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/arm64/dependencies/rhcos/pre-release/latest/rhcos-live-rootfs.aarch64.img",
		},
		{
			"openshift_version": "4.8",
			"cpu_architecture":  "x86_64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/latest/rhcos-live.x86_64.iso",
			"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/latest/rhcos-live-rootfs.x86_64.img",
		},
		{
			"openshift_version": "pre-release",
			"cpu_architecture":  "x86_64",
			"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/latest/rhcos-live.x86_64.iso",
			"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/latest/rhcos-live-rootfs.x86_64.img",
		},
	}

	assistedServer *ghttp.Server
	imageServer    *httptest.Server
	imageClient    *http.Client

	imageDir   string
	imageStore imagestore.ImageStore
)

func verifyIgnition(isoPath string, expectedContent []byte) {
	d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
	Expect(err).NotTo(HaveOccurred())
	fs, err := d.GetFilesystem(0)
	Expect(err).NotTo(HaveOccurred())

	f, err := fs.OpenFile("/images/ignition.img", os.O_RDONLY)
	Expect(err).NotTo(HaveOccurred())

	gzipReader, err := gzip.NewReader(f)
	Expect(err).NotTo(HaveOccurred())
	cpioReader := cpio.NewReader(gzipReader)
	hdr, err := cpioReader.Next()
	Expect(err).NotTo(HaveOccurred())
	Expect(hdr.Name).To(Equal("config.ign"))
	Expect(hdr.Size).To(Equal(int64(len(expectedContent))))

	content, err := ioutil.ReadAll(cpioReader)
	Expect(err).NotTo(HaveOccurred())
	Expect(content).To(Equal(expectedContent))
}

func verifyRamdisk(isoPath string, expectedContent []byte) {
	d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
	Expect(err).NotTo(HaveOccurred())
	fs, err := d.GetFilesystem(0)
	Expect(err).NotTo(HaveOccurred())

	f, err := fs.OpenFile("/images/assisted_installer_custom.img", os.O_RDONLY)
	Expect(err).NotTo(HaveOccurred())

	content, err := ioutil.ReadAll(f)
	Expect(err).NotTo(HaveOccurred())
	Expect(bytes.TrimRight(content, "\x00")).To(Equal(expectedContent))
}

func getImage(urlPath, imageType string, version map[string]string) string {
	resp, err := imageClient.Get(imageServer.URL + urlPath)
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))

	isoFile, err := ioutil.TempFile("", fmt.Sprintf("imageTest-%s-%s.%s.iso", version["openshift_version"], imageType, version["cpu_architecture"]))
	Expect(err).NotTo(HaveOccurred())
	_, err = io.Copy(isoFile, resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return isoFile.Name()
}

func startServers() {
	// Set up assisted service
	assistedServer = ghttp.NewServer()
	u, err := url.Parse(assistedServer.URL())
	Expect(err).NotTo(HaveOccurred())

	// Set up image handler
	reg := prometheus.NewRegistry()
	handler := handlers.NewImageHandler(imageStore, reg, u.Scheme, u.Host, "", "", 1)
	imageServer = httptest.NewServer(handler)
	imageClient = imageServer.Client()
}

var _ = Describe("Image integration tests", func() {
	var (
		isoFile         string
		imageID         string
		initrdContent   []byte
		ignitionContent = []byte("someignitioncontent")
	)

	Context("full-iso", func() {
		BeforeEach(func() {
			imageID = uuid.New().String()

			startServers()
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
			)
		})

		AfterEach(func() {
			assistedServer.Close()
			imageServer.Close()
			Expect(os.Remove(isoFile)).To(Succeed())
		})

		for i := range versions {
			version := versions[i]

			It("returns a full image", func() {
				By("getting an iso")
				path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageID, version["openshift_version"], imagestore.ImageTypeFull, version["cpu_architecture"])
				isoFile = getImage(path, "full-iso", version)

				By("verifying ignition content")
				verifyIgnition(isoFile, ignitionContent)
			})
		}
	})

	Context("minimal-iso with initrd", func() {
		BeforeEach(func() {
			imageID = uuid.New().String()
			initrdContent = []byte("someramdisk")

			startServers()
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
					ghttp.RespondWith(http.StatusOK, initrdContent),
				),
			)
		})

		AfterEach(func() {
			assistedServer.Close()
			imageServer.Close()
			Expect(os.Remove(isoFile)).To(Succeed())
		})

		for i := range versions {
			version := versions[i]
			It("returns a minimal image with initrd", func() {
				path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageID, version["openshift_version"], imagestore.ImageTypeMinimal, version["cpu_architecture"])
				isoFile = getImage(path, "minimal-iso-initrd", version)

				By("verifying ignition content")
				verifyIgnition(isoFile, ignitionContent)

				By("verifying ramdisk content")
				verifyRamdisk(isoFile, initrdContent)
			})
		}
	})

	Context("minimal-iso without initrd", func() {
		BeforeEach(func() {
			imageID = uuid.New().String()
			initrdContent = []byte("")

			startServers()
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
					ghttp.RespondWith(http.StatusOK, initrdContent),
				),
			)
		})

		AfterEach(func() {
			assistedServer.Close()
			imageServer.Close()
			Expect(os.Remove(isoFile)).To(Succeed())
		})

		for i := range versions {
			version := versions[i]
			It("returns a minimal image without initrd", func() {
				path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageID, version["openshift_version"], imagestore.ImageTypeMinimal, version["cpu_architecture"])
				isoFile = getImage(path, "minimal-iso-no-initrd", version)

				By("verifying ignition content")
				verifyIgnition(isoFile, ignitionContent)

				By("verifying ramdisk content")
				verifyRamdisk(isoFile, initrdContent)
			})
		}
	})
})

var _ = BeforeSuite(func() {
	var err error

	imageDir, err = ioutil.TempDir("", "imagesTest")
	Expect(err).To(BeNil())

	imageStore, err = imagestore.NewImageStore(isoeditor.NewEditor(imageDir), imageDir, versions)
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

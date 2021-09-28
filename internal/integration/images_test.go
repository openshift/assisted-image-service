package integration

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
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cavaliercoder/go-cpio"
	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
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

	assistedServer       *ghttp.Server
	imageIDWithInitRD    = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
	imageIDWithoutInitRD = "bf25292a-dddd-49dc-ab9c-3fb4c1f07072"
	ignitionContent      = []byte("someignitioncontent")
	initrdContent        = []byte("someramdisk")
	imageServer          *httptest.Server
	imageClient          *http.Client

	imageDir        string
	scratchSpaceDir string
	is              imagestore.ImageStore
	ctxBg           = context.Background()
)

func verifyIgnition(fs filesystem.FileSystem) {
	// TODO(djzager): Export isoeditor.ignitionImagePath?
	f, err := fs.OpenFile("/images/ignition.img", os.O_RDONLY)
	Expect(err).NotTo(HaveOccurred())

	gzipReader, err := gzip.NewReader(f)
	Expect(err).NotTo(HaveOccurred())
	cpioReader := cpio.NewReader(gzipReader)
	hdr, err := cpioReader.Next()
	Expect(err).NotTo(HaveOccurred())
	// TODO(djzager): Tie this to some constant?
	Expect(hdr.Name).To(Equal("config.ign"))
	Expect(hdr.Size).To(Equal(int64(len(ignitionContent))))

	content, err := ioutil.ReadAll(cpioReader)
	Expect(err).NotTo(HaveOccurred())
	Expect(content).To(Equal(ignitionContent))
}

func verifyRamdisk(fs filesystem.FileSystem, expectedContent []byte) {
	// TODO(djzager): Should we export isoeditor.ramDiskImagePath?
	f, err := fs.OpenFile("/images/assisted_installer_custom.img", os.O_RDONLY)
	Expect(err).NotTo(HaveOccurred())

	content, err := ioutil.ReadAll(f)
	Expect(err).NotTo(HaveOccurred())
	Expect(bytes.TrimRight(content, "\x00")).To(Equal(expectedContent))
}

func getImage(path, imageType string, version map[string]string) filesystem.FileSystem {
	resp, err := imageClient.Get(imageServer.URL + path)
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))

	isoPath := filepath.Join(scratchSpaceDir, fmt.Sprintf("test-%s-%s.%s.iso", version["openshift_version"], imageType, version["cpu_architecture"]))
	out, err := os.Create(isoPath)
	Expect(err).NotTo(HaveOccurred())
	_, err = io.Copy(out, resp.Body)
	Expect(err).NotTo(HaveOccurred())

	d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
	Expect(err).NotTo(HaveOccurred())
	fs, err := d.GetFilesystem(0)
	Expect(err).NotTo(HaveOccurred())

	return fs
}

var _ = Describe("Image integration tests", func() {
	Context("full-iso", func() {
		BeforeEach(func() {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageIDWithInitRD), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
			)
		})

		for _, version := range versions {
			It("returns a full image", func() {
				By("getting an iso image")
				path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageIDWithInitRD, version["openshift_version"], imagestore.ImageTypeFull, version["cpu_architecture"])
				fs := getImage(path, "full-iso", version)

				By("verifying ignition content")
				verifyIgnition(fs)
			})
		}
	})

	Context("minimal-iso with initrd", func() {
		BeforeEach(func() {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageIDWithInitRD), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageIDWithInitRD)),
					ghttp.RespondWith(http.StatusOK, initrdContent),
				),
			)
		})

		for _, version := range versions {
			It("returns a minimal image with initrd", func() {
				path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageIDWithInitRD, version["openshift_version"], imagestore.ImageTypeMinimal, version["cpu_architecture"])
				fs := getImage(path, "minimal-iso-initrd", version)

				By("verifying ignition content")
				verifyIgnition(fs)

				By("verifying ramdisk content")
				verifyRamdisk(fs, initrdContent)
			})
		}
	})

	Context("minimal-iso without initrd", func() {
		BeforeEach(func() {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageIDWithoutInitRD), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageIDWithoutInitRD)),
					ghttp.RespondWith(http.StatusOK, []byte("")),
				),
			)
		})

		for _, version := range versions {
			It("returns a minimal image without initrd", func() {
				path := fmt.Sprintf("/images/%s?version=%s&type=%s&arch=%s", imageIDWithoutInitRD, version["openshift_version"], imagestore.ImageTypeMinimal, version["cpu_architecture"])
				fs := getImage(path, "minimal-iso-no-initrd", version)

				By("verifying ignition content")
				verifyIgnition(fs)

				By("verifying ramdisk content")
				verifyRamdisk(fs, []byte(""))
			})
		}
	})
})

var _ = BeforeSuite(func() {
	var err error

	imageDir, err = ioutil.TempDir("", "imagesTest")
	Expect(err).To(BeNil())
	scratchSpaceDir, err = ioutil.TempDir("", "imagesTestScratch")
	Expect(err).NotTo(HaveOccurred())

	is, err = imagestore.NewImageStore(isoeditor.NewEditor(imageDir), imageDir, versions)
	Expect(err).NotTo(HaveOccurred())

	err = is.Populate(ctxBg)
	Expect(err).NotTo(HaveOccurred())

	// Set up assisted service
	assistedServer = ghttp.NewServer()
	u, err := url.Parse(assistedServer.URL())
	Expect(err).NotTo(HaveOccurred())

	// Set up image handler
	handler := handlers.NewImageHandler(is, u.Scheme, u.Host, "", "", 1)
	imageServer = httptest.NewServer(handler)
	imageClient = imageServer.Client()
})

var _ = AfterSuite(func() {
	Expect(os.RemoveAll(imageDir)).To(Succeed())
	Expect(os.RemoveAll(scratchSpaceDir)).To(Succeed())
	assistedServer.Close()
	imageServer.Close()
})

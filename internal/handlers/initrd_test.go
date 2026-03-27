package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"go.uber.org/mock/gomock"
)

var _ = Describe("ServeHTTP", func() {
	var (
		ctrl                 *gomock.Controller
		mockImageStore       *imagestore.MockImageStore
		imageFilename        string
		imageID              = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		assistedServer       *ghttp.Server
		ignitionContent      = []byte("someignitioncontent")
		minimalInitrdContent = []byte("someinitrdcontent")
		nmstatectlContent    = []byte("nmstatectlContent")
		// must match content in createTestISO
		initrdContent        = []byte("this is initrd")
		ignitionArchiveBytes = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 50, 48, 55, 48, 55, 48,
			52, 128, 0, 48, 109, 97, 232, 104, 98, 128, 29, 24, 162, 113, 141, 113,
			168, 67, 7, 78, 48, 70, 114, 126, 94, 90, 102, 186, 94, 102, 122, 30,
			3, 3, 3, 67, 113, 126, 110, 106, 102, 122, 94, 102, 73, 102, 126, 94,
			114, 126, 94, 73, 106, 94, 9, 3, 138, 123, 8, 1, 98, 213, 225, 116,
			79, 72, 144, 163, 167, 143, 107, 144, 162, 162, 34, 200, 61, 128, 0, 0,
			0, 255, 255, 191, 236, 44, 242, 12, 1, 0, 0, 0}
		server                   *httptest.Server
		client                   *http.Client
		lastModified             string
		header                   = http.Header{}
		workDir                  string
		nmstatectlPathForCaching string
	)

	BeforeEach(func() {
		var err error
		workDir, err = os.MkdirTemp("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())

		nmstatectlPathForCaching = filepath.Join(workDir, "nmstatectl-4.18--x86_64")
		err = os.WriteFile(nmstatectlPathForCaching, nmstatectlContent, 0600)
		Expect(err).NotTo(HaveOccurred())

		ctrl = gomock.NewController(GinkgoT())
		mockImageStore = imagestore.NewMockImageStore(ctrl)
		imageFilename = createTestISO()

		lastModified = "Fri, 22 Apr 2022 18:11:09 GMT"
		header.Set("Last-Modified", lastModified)
		assistedServer = ghttp.NewServer()
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
				ghttp.RespondWith(http.StatusOK, ignitionContent, header),
			),
		)
		u, err := url.Parse(assistedServer.URL())
		Expect(err).NotTo(HaveOccurred())

		asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
		Expect(err).NotTo(HaveOccurred())

		handler := &ImageHandler{
			initrd: &initrdHandler{
				ImageStore: mockImageStore,
				client:     asc,
			},
		}
		server = httptest.NewServer(handler.router(1))

		client = server.Client()
	})

	AfterEach(func() {
		assistedServer.Close()
		server.Close()
		os.Remove(imageFilename)
		Expect(os.RemoveAll(workDir)).To(Succeed())
	})

	mockImage := func(version, arch string) {
		mockImageStore.EXPECT().HaveVersion(version, arch).Return(true).AnyTimes()
		mockImageStore.EXPECT().PathForParams(imagestore.ImageTypeFull, version, arch).Return(imageFilename).AnyTimes()
	}

	expectSuccessfulResponse := func(resp *http.Response, content []byte) {
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("Content-Disposition")).To(Equal(fmt.Sprintf("attachment; filename=%s-initrd.img", imageID)))
		_, err := http.ParseTime(resp.Header.Get("Last-Modified"))
		Expect(err).NotTo(HaveOccurred())
		if lastModified != "" {
			Expect(resp.Header.Get("Last-Modified")).To(Equal(lastModified))
		}
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal(content))
	}

	withNoMinimalInitrd := func() {
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
				ghttp.RespondWith(http.StatusNoContent, []byte{}),
			),
		)
	}

	It("returns the correct content with minimal initrd", func() {
		mockImage("4.9", "x86_64")
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
				ghttp.RespondWith(http.StatusOK, minimalInitrdContent),
			),
		)
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.9&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, append(append(initrdContent, ignitionArchiveBytes...), minimalInitrdContent...))
	})

	It("returns the correct content without minimal initrd", func() {
		mockImage("4.9", "x86_64")
		withNoMinimalInitrd()
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.9&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, append(initrdContent, ignitionArchiveBytes...))
	})

	It("uses the default arch", func() {
		mockImage("4.9", "x86_64")
		withNoMinimalInitrd()
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.9", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, append(initrdContent, ignitionArchiveBytes...))
	})

	It("returns not found when the imageID doesn't parse", func() {
		resp, err := client.Get(fmt.Sprintf("%s/images/_%s/pxe-initrd?version=4.9&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("returns bad request when the version is not provided", func() {
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("returns bad request when the specified version is missing", func() {
		mockImageStore.EXPECT().HaveVersion("4.11", "x86_64").Return(false)
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.11&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("returns the response code from assisted-service when querying the minimal initrd fails", func() {
		mockImage("4.9", "x86_64")
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
				ghttp.RespondWith(http.StatusServiceUnavailable, "unavailable"),
			),
		)
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.9&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
	})

	It("returns a valid last-modified header when provided an invalid one", func() {
		lastModified = ""
		header.Set("Last-Modified", "somenonsense")
		assistedServer.SetHandler(0,
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
				ghttp.RespondWith(http.StatusOK, ignitionContent, header),
			),
		)

		mockImage("4.9", "x86_64")
		withNoMinimalInitrd()
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.9&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, append(initrdContent, ignitionArchiveBytes...))
	})

	It("returns the correct content with minimal initrd and the OCP version supports nmstatectl", func() {
		mockImage("4.18", "x86_64")
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
				ghttp.RespondWith(http.StatusOK, minimalInitrdContent),
			),
		)
		mockImageStore.EXPECT().NmstatectlPathForParams("4.18", "x86_64").Return(nmstatectlPathForCaching, nil).AnyTimes()
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.18&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, append(append(append(initrdContent, ignitionArchiveBytes...), minimalInitrdContent...), nmstatectlContent...))
	})
})

package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
)

var _ = Describe("ServeHTTP", func() {
	var (
		ctrl            *gomock.Controller
		mockImageStore  *imagestore.MockImageStore
		imageFilename   string
		imageID         = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		assistedServer  *ghttp.Server
		ignitionContent = []byte("someignitioncontent")
		// must match content in createTestISO
		initrdContent        = []byte("this is initrd")
		ignitionArchiveBytes = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 50, 48, 55, 48, 55, 48,
			52, 128, 0, 48, 109, 97, 232, 104, 98, 128, 29, 24, 162, 113, 141, 113,
			168, 67, 7, 78, 48, 70, 114, 126, 94, 90, 102, 186, 94, 102, 122, 30,
			3, 3, 3, 67, 113, 126, 110, 106, 102, 122, 94, 102, 73, 102, 126, 94,
			114, 126, 94, 73, 106, 94, 9, 3, 138, 123, 8, 1, 98, 213, 225, 116,
			79, 72, 144, 163, 167, 143, 107, 144, 162, 162, 34, 200, 61, 128, 0, 0,
			0, 255, 255, 191, 236, 44, 242, 12, 1, 0, 0}
		server *httptest.Server
		client *http.Client
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockImageStore = imagestore.NewMockImageStore(ctrl)
		imageFilename = createTestISO()

		assistedServer = ghttp.NewServer()
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
				ghttp.RespondWith(http.StatusOK, ignitionContent),
			),
		)
		u, err := url.Parse(assistedServer.URL())
		Expect(err).NotTo(HaveOccurred())

		server = httptest.NewServer(&initrdHandler{
			ImageStore: mockImageStore,
			client:     NewAssistedServiceClient(u.Scheme, u.Host, ""),
		})

		client = server.Client()
	})

	AfterEach(func() {
		assistedServer.Close()
		server.Close()
		os.Remove(imageFilename)
	})

	mockImage := func(version, arch string) {
		mockImageStore.EXPECT().HaveVersion(version, arch).Return(true).AnyTimes()
		mockImageStore.EXPECT().PathForParams(imagestore.ImageTypeFull, version, arch).Return(imageFilename).AnyTimes()
	}

	expectSuccessfulResponse := func(resp *http.Response, content []byte) {
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("Content-Disposition")).To(Equal(fmt.Sprintf("attachment; filename=%s-initrd.img", imageID)))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal(content))
	}

	It("returns the correct content", func() {
		mockImage("4.9", "x86_64")
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd?version=4.9&arch=x86_64", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, append(initrdContent, ignitionArchiveBytes...))
	})

	It("uses the default arch", func() {
		mockImage("4.9", "x86_64")
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
})

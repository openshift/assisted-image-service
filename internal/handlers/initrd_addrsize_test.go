package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
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
		initrdAddrsize       = []byte{
			1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0, 122}
		initrdAddrsizeWithNmstatectl = []byte{1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0, 156}
		server                       *httptest.Server
		client                       *http.Client
		lastModified                 string
		header                       = http.Header{}
		workDir                      string
		nmstatectlPathForCaching     string
	)

	BeforeEach(func() {
		var err error
		workDir, err = os.MkdirTemp("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())

		nmstatectlPathForCaching = filepath.Join(workDir, "nmstatectl-4.18--s390x")
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
			s390xInitrdAddrsize: &initrdAddrSizeHandler{
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
		mockImageStore.EXPECT().IsOVEImage(version, arch).Return(false).AnyTimes()
	}

	expectSuccessfulResponse := func(resp *http.Response, content []byte) {
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("Content-Disposition")).To(Equal(fmt.Sprintf("attachment; filename=%s-initrd.addrsize", imageID)))
		_, err := http.ParseTime(resp.Header.Get("Last-Modified"))
		Expect(err).NotTo(HaveOccurred())
		if lastModified != "" {
			Expect(resp.Header.Get("Last-Modified")).To(Equal(lastModified))
		}
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal(content), fmt.Sprintf("debug info: expected: %s, actual: %s", content, respContent))
	}

	withNoMinimalInitrd := func() {
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
				ghttp.RespondWith(http.StatusNoContent, []byte{}),
			),
		)
	}

	It("returns overlay initrd.addrsize", func() {
		lastModified = ""
		header.Set("Last-Modified", "somenonsense")
		assistedServer.SetHandler(0,
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
				ghttp.RespondWith(http.StatusOK, ignitionContent, header),
			),
		)

		mockImage("4.11", "s390x")
		withNoMinimalInitrd()
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/s390x-initrd-addrsize?version=4.11", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, initrdAddrsize)
	})
	It("returns the correct content with minimal initrd and the OCP version supports nmstatectl", func() {
		mockImage("4.18", "s390x")
		assistedServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
				ghttp.RespondWith(http.StatusOK, minimalInitrdContent),
			),
		)
		mockImageStore.EXPECT().NmstatectlPathForParams("4.18", "s390x").Return(nmstatectlPathForCaching, nil).AnyTimes()
		resp, err := client.Get(fmt.Sprintf("%s/images/%s/s390x-initrd-addrsize?version=4.18", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, initrdAddrsizeWithNmstatectl)
	})
})

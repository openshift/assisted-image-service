package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/carbonin/assisted-image-service/pkg/imagestore"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "handlers")
}

var _ = Describe("ServeHTTP", func() {
	var (
		ctrl           *gomock.Controller
		mockImageStore *imagestore.MockImageStore
		server         *httptest.Server
		client         *http.Client
		imageFile      *os.File
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockImageStore = imagestore.NewMockImageStore(ctrl)
		server = httptest.NewServer(&ImageHandler{ImageStore: mockImageStore})
		client = server.Client()

		var err error
		imageFile, err = os.CreateTemp("", "image_handler_test")
		Expect(err).NotTo(HaveOccurred())
		_, err = imageFile.Write([]byte("someisocontent"))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
		os.Remove(imageFile.Name())
	})

	mockImage := func(version string) {
		mockImageStore.EXPECT().HaveVersion(version).Return(true).AnyTimes()
		mockImageStore.EXPECT().BaseFile(version).Return(imageFile, nil).AnyTimes()
	}

	expectSuccessfulResponse := func(resp *http.Response, content []byte) {
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal(content))
	}

	It("returns an image", func() {
		mockImage("4.8")
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071?version=4.8")
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, []byte("someisocontent"))
	})

	It("fails for a non-existant version", func() {
		mockImageStore.EXPECT().HaveVersion("4.7").Return(false)
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071?version=4.7")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no version is supplied", func() {
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no image id is supplied", func() {
		resp, err := client.Get(server.URL + "/images/")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})
})

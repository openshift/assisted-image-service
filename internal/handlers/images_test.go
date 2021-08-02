package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
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
		fullImageFile  *os.File
		minImageFile   *os.File
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockImageStore = imagestore.NewMockImageStore(ctrl)
		server = httptest.NewServer(&ImageHandler{ImageStore: mockImageStore})
		client = server.Client()

		var err error
		fullImageFile, err = os.CreateTemp("", "image_handler_test")
		Expect(err).NotTo(HaveOccurred())
		_, err = fullImageFile.Write([]byte("someisocontent"))
		Expect(err).NotTo(HaveOccurred())

		minImageFile, err = os.CreateTemp("", "image_handler_test")
		Expect(err).NotTo(HaveOccurred())
		_, err = minImageFile.Write([]byte("minimalisocontent"))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
		os.Remove(fullImageFile.Name())
		os.Remove(minImageFile.Name())
	})

	mockImage := func(version, imageType string) {
		mockImageStore.EXPECT().HaveVersion(version).Return(true).AnyTimes()

		var imageFile *os.File
		switch imageType {
		case imagestore.ImageTypeFull:
			imageFile = fullImageFile
		case imagestore.ImageTypeMinimal:
			imageFile = minImageFile
		default:
			Fail("cannot mock with an unsupported image type")
		}
		mockImageStore.EXPECT().BaseFile(version, imageType).Return(imageFile, nil).AnyTimes()
	}

	expectSuccessfulResponse := func(resp *http.Response, content []byte) {
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal(content))
	}

	It("returns a full image", func() {
		mockImage("4.8", imagestore.ImageTypeFull)
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071?version=4.8&type=full")
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, []byte("someisocontent"))
	})

	It("returns a minimal image", func() {
		mockImage("4.8", imagestore.ImageTypeMinimal)
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071?version=4.8&type=minimal")
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, []byte("minimalisocontent"))
	})

	It("fails for a non-existant version", func() {
		mockImageStore.EXPECT().HaveVersion("4.7").Return(false)
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071?version=4.7&type=full")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no version is supplied", func() {
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071&?type=full")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no type is supplied", func() {
		mockImageStore.EXPECT().HaveVersion("4.8").Return(true)
		resp, err := client.Get(server.URL + "/images/bf25292a-dddd-49dc-ab9c-3fb4c1f07071&?version=4.8")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no image id is supplied", func() {
		resp, err := client.Get(server.URL + "/images/")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})
})

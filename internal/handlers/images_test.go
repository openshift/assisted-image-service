package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
		ctrl              *gomock.Controller
		mockImageStore    *imagestore.MockImageStore
		server            *httptest.Server
		assistedServer    *httptest.Server
		client            *http.Client
		fullImageFilename string
		minImageFilename  string
		imageID           = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockImageStore = imagestore.NewMockImageStore(ctrl)

		serveIgnitionFunc := func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			Expect(r.URL.Path).To(Equal(fmt.Sprintf("/v1/clusters/%s/downloads/files", imageID)))
			Expect(r.URL.RawQuery).To(Equal("file_name=discovery.ign"))
			_, err := io.WriteString(w, "someignitioncontent")
			Expect(err).NotTo(HaveOccurred())
		}

		assistedServer = httptest.NewServer(http.HandlerFunc(serveIgnitionFunc))
		u, err := url.Parse(assistedServer.URL)
		Expect(err).NotTo(HaveOccurred())

		mockImageStream := func(isoPath string, ignitionContent string) (io.ReadSeeker, error) {
			defer GinkgoRecover()
			Expect(ignitionContent).To(Equal("someignitioncontent"))
			return os.Open(isoPath)
		}

		handler := &ImageHandler{
			ImageStore:            mockImageStore,
			GenerateImageStream:   mockImageStream,
			AssistedServiceHost:   u.Host,
			AssistedServiceScheme: u.Scheme,
		}
		server = httptest.NewServer(handler)
		client = server.Client()

		fullImageFile, err := os.CreateTemp("", "image_handler_test")
		Expect(err).NotTo(HaveOccurred())
		_, err = fullImageFile.Write([]byte("someisocontent"))
		Expect(err).NotTo(HaveOccurred())
		Expect(fullImageFile.Sync()).To(Succeed())
		Expect(fullImageFile.Close()).To(Succeed())
		fullImageFilename = fullImageFile.Name()

		minImageFile, err := os.CreateTemp("", "image_handler_test")
		Expect(err).NotTo(HaveOccurred())
		_, err = minImageFile.Write([]byte("minimalisocontent"))
		Expect(err).NotTo(HaveOccurred())
		Expect(minImageFile.Sync()).To(Succeed())
		Expect(minImageFile.Close()).To(Succeed())
		minImageFilename = minImageFile.Name()
	})

	AfterEach(func() {
		server.Close()
		assistedServer.Close()
		os.Remove(fullImageFilename)
		os.Remove(minImageFilename)
	})

	mockImage := func(version, imageType string) {
		mockImageStore.EXPECT().HaveVersion(version).Return(true).AnyTimes()

		var imageFile string
		switch imageType {
		case imagestore.ImageTypeFull:
			imageFile = fullImageFilename
		case imagestore.ImageTypeMinimal:
			imageFile = minImageFilename
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
		path := fmt.Sprintf("/images/%s?version=4.8&type=full", imageID)
		resp, err := client.Get(server.URL + path)
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, []byte("someisocontent"))
	})

	It("returns a minimal image", func() {
		mockImage("4.8", imagestore.ImageTypeMinimal)
		path := fmt.Sprintf("/images/%s?version=4.8&type=minimal", imageID)
		resp, err := client.Get(server.URL + path)
		Expect(err).NotTo(HaveOccurred())
		expectSuccessfulResponse(resp, []byte("minimalisocontent"))
	})

	It("fails for a non-existant version", func() {
		mockImageStore.EXPECT().HaveVersion("4.7").Return(false)
		path := fmt.Sprintf("/images/%s?version=4.7&type=full", imageID)
		resp, err := client.Get(server.URL + path)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no version is supplied", func() {
		path := fmt.Sprintf("/images/%s?type=full", imageID)
		resp, err := client.Get(server.URL + path)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no type is supplied", func() {
		mockImageStore.EXPECT().HaveVersion("4.8").Return(true)
		path := fmt.Sprintf("/images/%s?version=4.8", imageID)
		resp, err := client.Get(server.URL + path)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("fails when no image id is supplied", func() {
		resp, err := client.Get(server.URL + "/images/")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})
})

var _ = Describe("parseImageID", func() {
	var imageID = "87bfb5e4-884b-4c00-a38a-f90f8b71effe"
	It("returns the correct ID", func() {
		path := filepath.Join("/images", imageID)
		id, err := parseImageID(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(imageID))
	})

	It("fails with the wrong path", func() {
		path := filepath.Join("/wat/images", imageID)
		_, err := parseImageID(path)
		Expect(err).To(HaveOccurred())
	})

	It("fails with no id", func() {
		_, err := parseImageID("/images/")
		Expect(err).To(HaveOccurred())
	})
})

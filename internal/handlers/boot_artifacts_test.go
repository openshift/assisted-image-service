package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
)

var _ = Describe("ServeHTTP", func() {
	var (
		ctrl              *gomock.Controller
		mockImageStore    *imagestore.MockImageStore
		server            *httptest.Server
		client            *http.Client
		fullImageFilename string
		kernelArtifact    = "kernel"
		rootfsArtifact    = "rootfs"
		insfileArtifact   = "ins-file"
		defaultArch       = "x86_64"
		s390xArch         = "s390x"
	)

	Context("with image files", func() {
		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockImageStore = imagestore.NewMockImageStore(ctrl)

			fullImageFilename = createTestISO()
			handler := &BootArtifactsHandler{
				ImageStore: mockImageStore,
			}
			server = httptest.NewServer(handler)
			client = server.Client()
		})

		AfterEach(func() {
			os.Remove(fullImageFilename)
			server.Close()
		})

		mockImage := func(version, imageType, arch string) {
			mockImageStore.EXPECT().HaveVersion(version, arch).Return(true).AnyTimes()
			imageFile := fullImageFilename
			mockImageStore.EXPECT().PathForParams(imageType, version, arch).Return(imageFile).AnyTimes()
		}

		expectSuccessfulResponse := func(resp *http.Response, content []byte, artifact string) {
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Disposition")).To(Equal(fmt.Sprintf("attachment; filename=%s", artifact)))
			respContent, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(respContent).To(Equal(content))
		}

		It("returns a kernel artifact", func() {
			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.8", kernelArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("this is kernel"), "vmlinuz")
		})

		It("uses the arch parameter", func() {
			mockImage("4.8", imagestore.ImageTypeFull, "arm64")
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.8&arch=arm64", rootfsArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("this is rootfs"), "rootfs.img")
		})

		It("returns a rootfs artifact", func() {
			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.8", rootfsArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("this is rootfs"), "rootfs.img")
		})

		It("returns a ins-file artifact", func() {
			mockImage("4.15", imagestore.ImageTypeFull, s390xArch)
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.15&arch=s390x", insfileArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("this is generic.ins"), "generic.ins")
		})

		It("Error: returns a ins-file artifact", func() {
			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.8&arch=x86_64", insfileArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("supports HEAD requests", func() {
			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.8", rootfsArtifact)
			resp, err := client.Head(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Disposition")).To(Equal("attachment; filename=rootfs.img"))
		})

		It("fails for a non-existent version", func() {
			mockImageStore.EXPECT().PathForParams(imagestore.ImageTypeFull, "4.7", defaultArch).Return("").AnyTimes()
			mockImageStore.EXPECT().HaveVersion("4.7", defaultArch).Return(false)
			path := fmt.Sprintf("/boot-artifacts/%s?version=4.7", rootfsArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("fails when no version is supplied", func() {
			path := fmt.Sprintf("/boot-artifacts/%s", rootfsArtifact)
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("fails when no artifact is supplied", func() {
			mockImageStore.EXPECT().HaveVersion("4.8", defaultArch).Return(true)
			path := "/boot-artifacts/?version=4.8"
			resp, err := client.Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("fails when non-existent artifact is supplied", func() {
			resp, err := client.Get(server.URL + "/boot-artifacts/")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("fails for unsupported methods", func() {
			reader := strings.NewReader(`{"stuff": "data"}`)
			resp, err := client.Post(server.URL+"/boot-artifacts/", "application/json", reader)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})
})

var _ = DescribeTable("parseArtifact",
	func(path, arch, version, artifact string, success bool) {
		a, err := parseArtifact(path, arch, version)
		if success {
			Expect(err).NotTo(HaveOccurred())
			Expect(a).To(Equal(artifact))
		} else {
			Expect(err).To(HaveOccurred())
		}
	},
	Entry("returns rootfs correctly", "/boot-artifacts/rootfs", "x86_64", "4.18.0", "rootfs.img", true),
	Entry("returns kernel correctly", "/boot-artifacts/kernel", "x86_64", "4.19.0", "vmlinuz", true),
	Entry("returns s390x kernel correctly", "/boot-artifacts/kernel", "s390x", "4.18.0", "kernel.img", true),
	Entry("fails for an invalid artifact", "/boot-artifacts/asdf", "x86_64", "4.19.0", "", false),
	Entry("fails for an incorrect path", "/wrong-path/rootfs", "x86_64", "4.18.0", "", false),
	Entry("returns generic.ins correctly", "/boot-artifacts/ins-file", "s390x", "4.18.0", "generic.ins", true),
	Entry("fails generic.ins incorrect arch", "/boot-artifacts/ins-file", "x86_64", "4.19.0", "", false),
)

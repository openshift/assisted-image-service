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
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

var _ = Describe("ServeHTTP", func() {
	var (
		ctrl              *gomock.Controller
		mockImageStore    *imagestore.MockImageStore
		fullImageFilename string
		minImageFilename  string
		imageID           = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		assistedServer    *ghttp.Server
		ignitionContent   = "someignitioncontent"
	)

	Context("with image files", func() {
		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockImageStore = imagestore.NewMockImageStore(ctrl)

			fullImageFile, err := os.CreateTemp("", "iso_handler_test")
			Expect(err).NotTo(HaveOccurred())
			_, err = fullImageFile.Write([]byte("someisocontent"))
			Expect(err).NotTo(HaveOccurred())
			Expect(fullImageFile.Sync()).To(Succeed())
			Expect(fullImageFile.Close()).To(Succeed())
			fullImageFilename = fullImageFile.Name()

			minImageFile, err := os.CreateTemp("", "iso_handler_test")
			Expect(err).NotTo(HaveOccurred())
			_, err = minImageFile.Write([]byte("minimalisocontent"))
			Expect(err).NotTo(HaveOccurred())
			Expect(minImageFile.Sync()).To(Succeed())
			Expect(minImageFile.Close()).To(Succeed())
			minImageFilename = minImageFile.Name()

			assistedServer = ghttp.NewServer()
		})

		AfterEach(func() {
			assistedServer.Close()
			os.Remove(fullImageFilename)
			os.Remove(minImageFilename)
		})

		mockImage := func(version, imageType, arch string) {
			mockImageStore.EXPECT().HaveVersion(version, arch).Return(true).AnyTimes()

			var imageFile string
			switch imageType {
			case imagestore.ImageTypeFull:
				imageFile = fullImageFilename
			case imagestore.ImageTypeMinimal:
				imageFile = minImageFilename
			default:
				Fail("cannot mock with an unsupported image type")
			}
			mockImageStore.EXPECT().PathForParams(imageType, version, arch).Return(imageFile).AnyTimes()
		}

		expectSuccessfulResponse := func(resp *http.Response, content []byte) {
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Disposition")).To(Equal(fmt.Sprintf("attachment; filename=%s-discovery.iso", imageID)))
			respContent, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(respContent).To(Equal(content))
		}

		Context("with no auth", func() {
			var (
				server        *httptest.Server
				client        *http.Client
				initrdContent []byte
			)

			BeforeEach(func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
						ghttp.RespondWith(http.StatusOK, ignitionContent),
					),
				)

				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				initrdContent = nil
				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					if isoPath == minImageFilename {
						Expect(ramdiskBytes).To(Equal(initrdContent))
					}
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &isoHandler{
					ImageStore:          mockImageStore,
					GenerateImageStream: mockImageStream,
					client:              asc,
				}
				server = httptest.NewServer(handler)
				client = server.Client()
			})

			AfterEach(func() {
				server.Close()
			})

			It("returns a full image", func() {
				mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
				path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
				resp, err := client.Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("uses the arch parameter", func() {
				mockImage("4.8", imagestore.ImageTypeFull, "arm64")
				path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso&arch=arm64", imageID)
				resp, err := client.Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("returns a minimal image with an initrd", func() {
				initrdContent = []byte("someramdisk")
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
						ghttp.RespondWith(http.StatusOK, initrdContent),
					),
				)
				mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch)
				path := fmt.Sprintf("/images/%s?version=4.8&type=minimal-iso", imageID)
				resp, err := client.Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("minimalisocontent"))
			})

			It("returns a minimal image with no initrd", func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
						ghttp.RespondWith(http.StatusNoContent, initrdContent),
					),
				)
				mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch)
				path := fmt.Sprintf("/images/%s?version=4.8&type=minimal-iso", imageID)
				resp, err := client.Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("minimalisocontent"))
			})

			It("fails for a non-existant version", func() {
				mockImageStore.EXPECT().HaveVersion("4.7", defaultArch).Return(false)
				path := fmt.Sprintf("/images/%s?version=4.7&type=full-iso", imageID)
				resp, err := client.Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})

			It("fails when no version is supplied", func() {
				path := fmt.Sprintf("/images/%s?type=full-iso", imageID)
				resp, err := client.Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})

			It("fails when no type is supplied", func() {
				mockImageStore.EXPECT().HaveVersion("4.8", defaultArch).Return(true)
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

		It("passes Authorization header through to assisted requests", func() {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
					ghttp.VerifyHeader(http.Header{"Authorization": []string{"Bearer mytoken"}}),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
			)

			u, err := url.Parse(assistedServer.URL())
			Expect(err).NotTo(HaveOccurred())

			mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes []byte) (isoeditor.ImageReader, error) {
				defer GinkgoRecover()
				Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
				return os.Open(isoPath)
			}

			asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
			Expect(err).NotTo(HaveOccurred())

			handler := &isoHandler{
				ImageStore:          mockImageStore,
				GenerateImageStream: mockImageStream,
				client:              asc,
			}
			server := httptest.NewServer(handler)
			defer server.Close()

			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
			req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer mytoken")

			resp, err := server.Client().Do(req)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("someisocontent"))
		})

		It("passes api_key param through to assisted requests", func() {
			assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", assistedPath, "file_name=discovery.ign&api_key=mytoken"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
			)

			u, err := url.Parse(assistedServer.URL())
			Expect(err).NotTo(HaveOccurred())

			mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes []byte) (isoeditor.ImageReader, error) {
				defer GinkgoRecover()
				Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
				return os.Open(isoPath)
			}

			asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
			Expect(err).NotTo(HaveOccurred())

			handler := &isoHandler{
				ImageStore:          mockImageStore,
				GenerateImageStream: mockImageStream,
				client:              asc,
			}
			server := httptest.NewServer(handler)
			defer server.Close()

			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso&api_key=mytoken", imageID)
			resp, err := server.Client().Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("someisocontent"))
		})

		It("passes image_token param through to assisted requests header", func() {
			assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", assistedPath, "file_name=discovery.ign"),
					ghttp.VerifyHeader(http.Header{"Image-Token": []string{"mytoken"}}),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
			)

			u, err := url.Parse(assistedServer.URL())
			Expect(err).NotTo(HaveOccurred())

			mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes []byte) (isoeditor.ImageReader, error) {
				defer GinkgoRecover()
				Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
				return os.Open(isoPath)
			}

			asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
			Expect(err).NotTo(HaveOccurred())

			handler := &isoHandler{
				ImageStore:          mockImageStore,
				GenerateImageStream: mockImageStream,
				client:              asc,
			}
			server := httptest.NewServer(handler)
			defer server.Close()

			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso&image_token=mytoken", imageID)
			resp, err := server.Client().Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			expectSuccessfulResponse(resp, []byte("someisocontent"))
		})

		It("returns an auth failure if assisted auth fails when querying ignition", func() {
			assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", assistedPath, "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusUnauthorized, ""),
				),
			)

			u, err := url.Parse(assistedServer.URL())
			Expect(err).NotTo(HaveOccurred())

			asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
			Expect(err).NotTo(HaveOccurred())

			handler := &isoHandler{
				ImageStore: mockImageStore,
				client:     asc,
			}
			server := httptest.NewServer(handler)
			defer server.Close()

			mockImage("4.8", imagestore.ImageTypeFull, defaultArch)
			path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
			resp, err := server.Client().Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("returns an auth failure if assisted auth fails when querying initrd", func() {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "file_name=discovery.ign"),
					ghttp.RespondWith(http.StatusOK, ignitionContent),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID),
					),
					ghttp.RespondWith(http.StatusUnauthorized, ""),
				),
			)

			u, err := url.Parse(assistedServer.URL())
			Expect(err).NotTo(HaveOccurred())

			asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
			Expect(err).NotTo(HaveOccurred())

			handler := &isoHandler{
				ImageStore: mockImageStore,
				client:     asc,
			}
			server := httptest.NewServer(handler)
			defer server.Close()

			mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch)
			path := fmt.Sprintf("/images/%s?version=4.8&type=minimal-iso", imageID)
			resp, err := server.Client().Get(server.URL + path)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
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

var _ = Describe("readiness handler", func() {
	It("Not ready to Ready", func() {
		readinessHandler := NewReadinessHandler()
		server := httptest.NewServer(readinessHandler)
		defer server.Close()

		resp, err := server.Client().Get(server.URL + "/ready")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))

		By("Enable readiness handler")
		readinessHandler.Enable()

		resp, err = server.Client().Get(server.URL + "/ready")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

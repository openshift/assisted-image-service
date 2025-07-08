package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

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
		lastModified      string
		header            = http.Header{}

		// generated at https://jwt.io/ with payload:
		//
		//	{
		//		"infra_env_id": "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		//	}
		tokenInfraEnv = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJiZjI1MjkyYS1kZGRkLTQ5ZGMtYWI5Yy0zZmI0YzFmMDcwNzEifQ.VbI4JtIVcxy2S7n2tYFgtFPD9St15RrzQnpJuE0CuAI" //#nosec
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

			lastModified = "Fri, 22 Apr 2022 18:11:09 GMT"
			header.Set("Last-Modified", lastModified)
		})

		AfterEach(func() {
			assistedServer.Close()
			os.Remove(fullImageFilename)
			os.Remove(minImageFilename)
		})

		mockImage := func(version, imageType, arch string, isOVE bool) {
			mockImageStore.EXPECT().HaveVersion(version, arch).Return(true).AnyTimes()
			mockImageStore.EXPECT().IsOVEImage(version, arch).Return(isOVE).AnyTimes()

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
			_, err := http.ParseTime(resp.Header.Get("Last-Modified"))
			Expect(err).NotTo(HaveOccurred())
			if lastModified != "" {
				Expect(resp.Header.Get("Last-Modified")).To(Equal(lastModified))
			}
			respContent, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(respContent).To(Equal(content))
		}

		initIgnitionHandler := func(queryString string) {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), queryString),
					ghttp.RespondWith(http.StatusOK, ignitionContent, header),
				),
			)
		}

		setInfraenvKargsHandlerSuccess := func(args ...string) {
			var response string
			if len(args) == 0 {
				response = "{}"
			} else {
				discoveryKernelArguments, err := isoeditor.KargsToStr(args)
				Expect(err).ToNot(HaveOccurred())
				var infraEnv struct {
					// JSON formatted string array representing the discovery image kernel arguments.
					KernelArguments *string `json:"kernel_arguments,omitempty"`
				}
				infraEnv.KernelArguments = &discoveryKernelArguments
				b, err := json.Marshal(&infraEnv)
				Expect(err).ToNot(HaveOccurred())
				response = string(b)
			}
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf(infraEnvPathFormat, imageID)),
					ghttp.RespondWith(http.StatusOK, response, header),
				),
			)
		}

		setInfraenvKargsHandlerFailure := func() {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf(infraEnvPathFormat, imageID)),
					ghttp.RespondWith(http.StatusInternalServerError, "", header),
				),
			)
		}

		Describe("Short URLs", func() {

			Context("with no auth", func() {
				var (
					server        *httptest.Server
					client        *http.Client
					initrdContent []byte
				)

				BeforeEach(func() {
					u, err := url.Parse(assistedServer.URL())
					Expect(err).NotTo(HaveOccurred())

					initrdContent = nil
					mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
						defer GinkgoRecover()
						Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
						if isoPath == minImageFilename {
							Expect(ramdiskBytes).To(Equal(initrdContent))
						}
						return os.Open(isoPath)
					}

					asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
					Expect(err).NotTo(HaveOccurred())

					handler := &ImageHandler{
						long: &isoHandler{
							ImageStore:          mockImageStore,
							GenerateImageStream: mockImageStream,
							client:              asc,
							urlParser:           parseLongURL,
						},
						byID: &isoHandler{
							ImageStore:          mockImageStore,
							GenerateImageStream: mockImageStream,
							client:              asc,
							urlParser:           parseShortURL,
						},
					}
					server = httptest.NewServer(handler.router(1))
					client = server.Client()
				})

				AfterEach(func() {
					server.Close()
				})

				It("returns a full image", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
					path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("uses the arch parameter", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.8", imagestore.ImageTypeFull, "arm64", false)
					path := fmt.Sprintf("/byid/%s/4.8/arm64/full.iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("returns a minimal image with an initrd", func() {
					initIgnitionHandler("discovery_iso_type=minimal-iso&file_name=discovery.ign")
					initrdContent = []byte("someramdisk")
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusOK, initrdContent),
						),
					)
					setInfraenvKargsHandlerSuccess()
					mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch, false)
					path := fmt.Sprintf("/byid/%s/4.8/x86_64/minimal.iso", imageID)
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("minimalisocontent"))
				})

				It("returns a minimal image with no initrd", func() {
					initIgnitionHandler("discovery_iso_type=minimal-iso&file_name=discovery.ign")
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusNoContent, initrdContent),
						),
					)
					mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch, false)
					path := fmt.Sprintf("/byid/%s/4.8/x86_64/minimal.iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("minimalisocontent"))
				})

				It("requests ove.ign for OVE images", func() {
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID),
								"discovery_iso_type=full-iso&file_name=ove.ign"),
							ghttp.RespondWith(http.StatusOK, ignitionContent, header),
						),
					)

					mockImageStore.EXPECT().HaveVersion("4.14", defaultArch).Return(true)
					mockImageStore.EXPECT().IsOVEImage("4.14", defaultArch).Return(true)
					mockImageStore.EXPECT().PathForParams(imagestore.ImageTypeFull, "4.14", defaultArch).Return(fullImageFilename)

					setInfraenvKargsHandlerSuccess()
					path := fmt.Sprintf("/byid/%s/4.14/x86_64/full.iso", imageID)
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("fails for a non-existant version", func() {
					mockImageStore.EXPECT().HaveVersion("4.7", defaultArch).Return(false)
					path := fmt.Sprintf("/byid/%s/4.7/x86_64/full.iso", imageID)
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				})

				It("fails when no type is supplied", func() {
					mockImageStore.EXPECT().HaveVersion("4.8", defaultArch).Return(true)
					path := fmt.Sprintf("/byid/%s/4.8/x86_64/", imageID)
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				})

				It("fails when no image id is supplied", func() {
					resp, err := client.Get(server.URL + "/byid/")
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				})

				It("returns a valid last-modified header when provided an invalid one", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					lastModified = ""
					header.Set("Last-Modified", "somenonsense")
					mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
					path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("fail kargs infra env query", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
					path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso", imageID)
					setInfraenvKargsHandlerFailure()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
				})

				It("fails when kargs are supplied for an s390x image", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.11", imagestore.ImageTypeFull, "s390x", false)
					path := fmt.Sprintf("/byid/%s/4.11/s390x/full.iso", imageID)
					setInfraenvKargsHandlerSuccess("arg")
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				})
			})

			It("passes Authorization header through to assisted requests", func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "discovery_iso_type=full-iso&file_name=discovery.ign"),
						ghttp.VerifyHeader(http.Header{"Authorization": []string{"Bearer mytoken"}}),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)
				setInfraenvKargsHandlerSuccess()

				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					byID: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso", imageID)
				req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
				Expect(err).NotTo(HaveOccurred())
				req.Header.Set("Authorization", "Bearer mytoken")

				resp, err := server.Client().Do(req)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("passes api_key query param through to assisted requests", func() {
				assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", assistedPath, "discovery_iso_type=full-iso&file_name=discovery.ign&api_key=mytoken"),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)

				setInfraenvKargsHandlerSuccess()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					byID: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso?api_key=mytoken", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("passes api_key url path segment through to assisted requests", func() {
				assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", assistedPath, fmt.Sprintf("discovery_iso_type=full-iso&file_name=discovery.ign&api_key=%s", tokenInfraEnv)),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)

				setInfraenvKargsHandlerSuccess()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					byAPIKey: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/byapikey/%s/4.8/x86_64/full.iso", tokenInfraEnv)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("passes non empty kargs", func() {
				initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
				kernelArguments := []string{
					"p1",
					"p2",
					"p3",
				}
				setInfraenvKargsHandlerSuccess(kernelArguments...)
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(kargs).To(Equal([]byte(" " + strings.Join(kernelArguments, " ") + "\n")))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					byID: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It("passes image_token param through to assisted requests header", func() {
				assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
				// generated at https://jwt.io/ with payload:
				// {
				// 	"infra_env_id": "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
				// }
				token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJiZjI1MjkyYS1kZGRkLTQ5ZGMtYWI5Yy0zZmI0YzFmMDcwNzEifQ.VbI4JtIVcxy2S7n2tYFgtFPD9St15RrzQnpJuE0CuAI" //#nosec
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", assistedPath, "discovery_iso_type=full-iso&file_name=discovery.ign"),
						ghttp.VerifyHeader(http.Header{"Image-Token": []string{token}}),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)

				setInfraenvKargsHandlerSuccess()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					byToken: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/bytoken/%s/4.8/x86_64/full.iso", token)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("returns an auth failure if assisted auth fails when querying ignition", func() {
				assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", assistedPath, "discovery_iso_type=full-iso&file_name=discovery.ign"),
						ghttp.RespondWith(http.StatusUnauthorized, ""),
					),
				)

				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					byID: &isoHandler{
						ImageStore: mockImageStore,
						client:     asc,
						urlParser:  parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/byid/%s/4.8/x86_64/full.iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})

			It("returns an auth failure if assisted auth fails when querying initrd", func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "discovery_iso_type=minimal-iso&file_name=discovery.ign"),
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

				handler := &ImageHandler{
					byID: &isoHandler{
						ImageStore: mockImageStore,
						client:     asc,
						urlParser:  parseShortURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch, false)
				path := fmt.Sprintf("/byid/%s/4.8/x86_64/minimal.iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})
		})

		Describe("Long URLs", func() {

			Context("with no auth", func() {
				var (
					server        *httptest.Server
					client        *http.Client
					initrdContent []byte
				)

				BeforeEach(func() {
					u, err := url.Parse(assistedServer.URL())
					Expect(err).NotTo(HaveOccurred())

					initrdContent = nil
					mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
						defer GinkgoRecover()
						Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
						if isoPath == minImageFilename {
							Expect(ramdiskBytes).To(Equal(initrdContent))
						}
						return os.Open(isoPath)
					}

					asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
					Expect(err).NotTo(HaveOccurred())

					handler := &ImageHandler{
						long: &isoHandler{
							ImageStore:          mockImageStore,
							GenerateImageStream: mockImageStream,
							client:              asc,
							urlParser:           parseLongURL,
						},
					}
					server = httptest.NewServer(handler.router(1))
					client = server.Client()
				})

				AfterEach(func() {
					server.Close()
				})

				It("returns a full image", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
					path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("uses the arch parameter", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.8", imagestore.ImageTypeFull, "arm64", false)
					path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso&arch=arm64", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("returns a minimal image with an initrd", func() {
					initIgnitionHandler("discovery_iso_type=minimal-iso&file_name=discovery.ign")
					initrdContent = []byte("someramdisk")
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusOK, initrdContent),
						),
					)
					setInfraenvKargsHandlerSuccess()
					mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch, false)
					path := fmt.Sprintf("/images/%s?version=4.8&type=minimal-iso", imageID)
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("minimalisocontent"))
				})

				It("returns a minimal image with no initrd", func() {
					initIgnitionHandler("discovery_iso_type=minimal-iso&file_name=discovery.ign")
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusNoContent, initrdContent),
						),
					)
					mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch, false)
					path := fmt.Sprintf("/images/%s?version=4.8&type=minimal-iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("minimalisocontent"))
				})

				It("fails for a non-existant version", func() {
					mockImageStore.EXPECT().HaveVersion("4.7", defaultArch).Return(false)
					path := fmt.Sprintf("/images/%s?version=4.7&type=full-iso", imageID)
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
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

				It("returns a valid last-modified header when provided an invalid one", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					lastModified = ""
					header.Set("Last-Modified", "somenonsense")
					mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
					path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
					setInfraenvKargsHandlerSuccess()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					expectSuccessfulResponse(resp, []byte("someisocontent"))
				})

				It("fail kargs infra env query", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
					path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
					setInfraenvKargsHandlerFailure()
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
				})

				It("fails when kargs are supplied for an s390x image", func() {
					initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
					mockImage("4.11", imagestore.ImageTypeFull, "s390x", false)
					path := fmt.Sprintf("/images/%s?version=4.11&type=full-iso&arch=s390x", imageID)
					setInfraenvKargsHandlerSuccess("arg")
					resp, err := client.Get(server.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				})
			})

			It("passes Authorization header through to assisted requests", func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "discovery_iso_type=full-iso&file_name=discovery.ign"),
						ghttp.VerifyHeader(http.Header{"Authorization": []string{"Bearer mytoken"}}),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)
				setInfraenvKargsHandlerSuccess()

				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
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
						ghttp.VerifyRequest("GET", assistedPath, "discovery_iso_type=full-iso&file_name=discovery.ign&api_key=mytoken"),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)

				setInfraenvKargsHandlerSuccess()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso&api_key=mytoken", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("passes non empty kargs", func() {
				initIgnitionHandler("discovery_iso_type=full-iso&file_name=discovery.ign")
				kernelArguments := []string{
					"p1",
					"p2",
					"p3",
				}
				setInfraenvKargsHandlerSuccess(kernelArguments...)
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(kargs).To(Equal([]byte(" " + strings.Join(kernelArguments, " ") + "\n")))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It("passes image_token param through to assisted requests header", func() {
				assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", assistedPath, "discovery_iso_type=full-iso&file_name=discovery.ign"),
						ghttp.VerifyHeader(http.Header{"Image-Token": []string{"mytoken"}}),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)

				setInfraenvKargsHandlerSuccess()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso&image_token=mytoken", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

			It("returns an auth failure if assisted auth fails when querying ignition", func() {
				assistedPath := fmt.Sprintf(fileRouteFormat, imageID)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", assistedPath, "discovery_iso_type=full-iso&file_name=discovery.ign"),
						ghttp.RespondWith(http.StatusUnauthorized, ""),
					),
				)

				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore: mockImageStore,
						client:     asc,
						urlParser:  parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeFull, defaultArch, false)
				path := fmt.Sprintf("/images/%s?version=4.8&type=full-iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})

			It("returns an auth failure if assisted auth fails when querying initrd", func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "discovery_iso_type=minimal-iso&file_name=discovery.ign"),
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

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore: mockImageStore,
						client:     asc,
						urlParser:  parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				mockImage("4.8", imagestore.ImageTypeMinimal, defaultArch, false)
				path := fmt.Sprintf("/images/%s?version=4.8&type=minimal-iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})
		})

		Context("OVE image handling", func() {
			It("fetches ove.ign for OVE images", func() {
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf(fileRouteFormat, imageID), "discovery_iso_type=full-iso&file_name=ove.ign"),
						ghttp.RespondWith(http.StatusOK, ignitionContent, header),
					),
				)
				setInfraenvKargsHandlerSuccess()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				mockImageStream := func(isoPath string, ignition *isoeditor.IgnitionContent, ramdiskBytes, kargs []byte) (isoeditor.ImageReader, error) {
					defer GinkgoRecover()
					Expect(ignition.Config).To(Equal([]byte(ignitionContent)))
					return os.Open(isoPath)
				}

				asc, err := NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				handler := &ImageHandler{
					long: &isoHandler{
						ImageStore:          mockImageStore,
						GenerateImageStream: mockImageStream,
						client:              asc,
						urlParser:           parseLongURL,
					},
				}
				server := httptest.NewServer(handler.router(1))
				defer server.Close()

				// Mock as OVE image
				mockImage("4.19", imagestore.ImageTypeFull, defaultArch, true)

				path := fmt.Sprintf("/images/%s?version=4.19&type=full-iso", imageID)
				resp, err := server.Client().Get(server.URL + path)
				Expect(err).NotTo(HaveOccurred())

				expectSuccessfulResponse(resp, []byte("someisocontent"))
			})

		})
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

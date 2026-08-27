package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cavaliercoder/go-cpio"
	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/google/uuid"
	"github.com/onsi/gomega/ghttp"
	"github.com/slok/go-http-metrics/middleware"

	"github.com/openshift/assisted-image-service/internal/common"
	"github.com/openshift/assisted-image-service/internal/handlers"
	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

var (
	versions = []imagestore.OSImage{
		{
			OpenshiftVersion: "4.17.0-ec.1",
			CPUArchitecture:  "arm64",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/arm64/dependencies/rhcos/pre-release/latest/rhcos-live-iso.aarch64.iso",
			Version:          "arm-latest",
		},
		{
			OpenshiftVersion: "4.8",
			CPUArchitecture:  "x86_64",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/latest/rhcos-live.x86_64.iso",
			Version:          "4.8-latest",
		},
		{
			OpenshiftVersion: "4.10.0-rc.0",
			CPUArchitecture:  "x86_64",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/4.10.0-rc.0/rhcos-live.x86_64.iso",
			Version:          "x86_64-latest",
		},
		{
			OpenshiftVersion: "4.11",
			CPUArchitecture:  "x86_64",
			URL:              "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/x86_64/fedora-coreos-35.20220103.3.0-live.x86_64.iso",
			Version:          "x86_64-latest",
		},
		{
			OpenshiftVersion: "4.11",
			CPUArchitecture:  "arm64",
			URL:              "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/35.20220103.3.0/aarch64/fedora-coreos-35.20220103.3.0-live.aarch64.iso",
			Version:          "arm-latest",
		},
		{
			OpenshiftVersion: "4.11",
			CPUArchitecture:  "s390x",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/s390x/dependencies/rhcos/4.11/4.11.9/rhcos-4.11.9-s390x-live.s390x.iso",
			Version:          "s390x-latest",
		},
		{
			OpenshiftVersion: "4.11",
			CPUArchitecture:  "ppc64le",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/ppc64le/dependencies/rhcos/4.11/4.11.9/rhcos-4.11.9-ppc64le-live.ppc64le.iso",
			Version:          "ppc64le-latest",
		},
		{
			OpenshiftVersion: "4.13",
			CPUArchitecture:  "x86_64",
			URL:              "https://okd-scos.s3.amazonaws.com/okd-scos/builds/413.9.202302280609-0/x86_64/scos-413.9.202302280609-0-live.x86_64.iso",
			Version:          "x86_64-latest",
		},
		{
			OpenshiftVersion: "4.18",
			CPUArchitecture:  "s390x",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/s390x/dependencies/rhcos/4.18/4.18.1/rhcos-4.18.1-s390x-live.s390x.iso",
			Version:          "s390x-418",
		},
		{
			OpenshiftVersion: "4.18",
			CPUArchitecture:  "x86_64",
			URL:              "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.18/4.18.1/rhcos-4.18.1-x86_64-live.x86_64.iso",
			Version:          "x86_64-418",
		},
	}

	imageDir            string
	imageStore          imagestore.ImageStore
	imageServiceBaseURL = "http://images.example.com"
)

var _ = Describe("Image integration tests", func() {
	var (
		isoFilename    string
		imageID        string
		assistedServer *ghttp.Server
		imageServer    *httptest.Server
		imageClient    *http.Client
	)

	buildInfraenvResponse := func(args ...string) []byte {
		if len(args) == 0 {
			return []byte("{}")
		}
		var infraEnv struct {
			// JSON formatted string array representing the discovery image kernel arguments.
			KernelArguments *string `json:"kernel_arguments,omitempty"`
		}
		kargs, err := isoeditor.KargsToStr(args)
		Expect(err).ToNot(HaveOccurred())
		infraEnv.KernelArguments = &kargs
		b, err := json.Marshal(&infraEnv)
		Expect(err).ToNot(HaveOccurred())
		return b
	}

	testcases := []struct {
		name               string
		fileName           string
		imageType          string
		expectedIgnition   []byte
		expectedRamdisk    []byte
		expectedExtraKargs []string
	}{
		{
			name:             "full-iso",
			imageType:        imagestore.ImageTypeFull,
			fileName:         "full.iso",
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  nil,
		},
		{
			name:               "full-iso-with-kargs",
			imageType:          imagestore.ImageTypeFull,
			fileName:           "full.iso",
			expectedIgnition:   []byte("someignitioncontent"),
			expectedRamdisk:    nil,
			expectedExtraKargs: []string{"p1", "p1", `key=value`},
		},
		{
			name:             "minimal-iso-with-initrd",
			imageType:        imagestore.ImageTypeMinimal,
			fileName:         "minimal.iso",
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  []byte("someramdiskcontent"),
		},
		{
			name:             "minimal-iso-without-initrd",
			imageType:        imagestore.ImageTypeMinimal,
			fileName:         "minimal.iso",
			expectedIgnition: []byte("someignitioncontent"),
			expectedRamdisk:  []byte(""),
		},
		{
			name:               "minimal-iso-without-initrd-with-kargs",
			imageType:          imagestore.ImageTypeMinimal,
			fileName:           "minimal.iso",
			expectedIgnition:   []byte("someignitioncontent"),
			expectedRamdisk:    []byte(""),
			expectedExtraKargs: []string{"p5", "p6", `key=value`},
		},
	}

	for i := range testcases {
		tc := testcases[i]

		Context(tc.name, func() {
			BeforeEach(func() {
				imageID = uuid.New().String()

				// Set up assisted service
				assistedServer = ghttp.NewServer()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())
				queryString := fmt.Sprintf("file_name=discovery.ign&discovery_iso_type=%s", tc.imageType)
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), queryString),
						ghttp.RespondWith(http.StatusOK, tc.expectedIgnition),
					),
				)
				if tc.expectedRamdisk != nil {
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusOK, tc.expectedRamdisk),
						),
					)
				}
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s", imageID)),
						ghttp.RespondWith(http.StatusOK, buildInfraenvResponse(tc.expectedExtraKargs...)),
					),
				)

				asc, err := handlers.NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				mdw := middleware.New(middleware.Config{})
				imageServer = httptest.NewServer(handlers.NewImageHandler(imageStore, asc, 1, mdw))
				imageClient = imageServer.Client()
			})

			AfterEach(func() {
				assistedServer.Close()
				imageServer.Close()
				Expect(os.Remove(isoFilename)).To(Succeed())
			})

			for i := range versions {
				version := versions[i]

				It("returns a properly generated "+tc.name+" iso image "+version.Version, func() {
					if version.CPUArchitecture == "s390x" {
						if tc.imageType == imagestore.ImageTypeMinimal {
							Skip("minimal ISO is not supported for s390x architecture")
						}
						if tc.expectedExtraKargs != nil {
							Skip("Karg editing is not supported for s390x architecture")
						}
					}

					By("getting an iso")
					path := fmt.Sprintf("/byid/%s/%s/%s/%s", imageID, version.OpenshiftVersion, version.CPUArchitecture, tc.fileName)
					resp, err := imageClient.Get(imageServer.URL + path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					isoFile, err := os.CreateTemp("", fmt.Sprintf("imageTest-%s-%s.%s.iso", version.OpenshiftVersion, tc.name, version.CPUArchitecture))
					Expect(err).NotTo(HaveOccurred())
					_, err = io.Copy(isoFile, resp.Body)
					Expect(err).NotTo(HaveOccurred())
					isoFilename = isoFile.Name()

					By("opening the iso")
					d, err := diskfs.Open(isoFilename, diskfs.WithOpenMode(diskfs.ReadOnly))
					Expect(err).NotTo(HaveOccurred())
					fs, err := d.GetFilesystem(0)
					Expect(err).NotTo(HaveOccurred())

					By("verifying ignition content")
					rc, err := ignitionPayloadReader(fs, version)
					Expect(err).NotTo(HaveOccurred())
					defer rc.Close()

					got, err := readIgnitionContentFromGzCpio(rc)
					Expect(err).NotTo(HaveOccurred())
					Expect(got).To(Equal(tc.expectedIgnition))

					if len(tc.expectedRamdisk) > 0 {
						By("verifying ramdisk content")
						f, err := fs.OpenFile("/images/assisted_installer_custom.img", os.O_RDONLY)
						Expect(err).NotTo(HaveOccurred())

						content, err := io.ReadAll(f)
						Expect(err).NotTo(HaveOccurred())
						Expect(bytes.TrimRight(content, "\x00")).To(Equal(tc.expectedRamdisk))
					}
					if len(tc.expectedExtraKargs) > 0 {
						By("verifying kernel arguments content")
						files, err := isoeditor.KargsFiles(isoFilename)
						Expect(err).ToNot(HaveOccurred())
						for _, fname := range files {
							f, err := fs.OpenFile(fname, os.O_RDONLY)
							Expect(err).ToNot(HaveOccurred())
							b, err := io.ReadAll(f)
							Expect(err).NotTo(HaveOccurred())
							Expect(string(b)).To(MatchRegexp(" " + strings.Join(tc.expectedExtraKargs, " ") + "\n#+ COREOS_KARG_EMBED_AREA"))
						}
					}
				})
			}
		})
		Context("nmstate archive verification - "+tc.name, func() {
			BeforeEach(func() {
				imageID = uuid.New().String()

				// Set up assisted service
				assistedServer = ghttp.NewServer()
				u, err := url.Parse(assistedServer.URL())
				Expect(err).NotTo(HaveOccurred())

				// pxe-initrd path: ignition without discovery_iso_type
				assistedServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", imageID), "file_name=discovery.ign"),
						ghttp.RespondWith(http.StatusOK, tc.expectedIgnition),
					),
				)
				// pxe-initrd path: minimal-initrd always requested; 200 when provided, else 204
				if len(tc.expectedRamdisk) > 0 {
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusOK, tc.expectedRamdisk),
						),
					)
				} else {
					assistedServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID)),
							ghttp.RespondWith(http.StatusNoContent, nil),
						),
					)
				}

				asc, err := handlers.NewAssistedServiceClient(u.Scheme, u.Host, "")
				Expect(err).NotTo(HaveOccurred())

				mdw := middleware.New(middleware.Config{})
				imageServer = httptest.NewServer(handlers.NewImageHandler(imageStore, asc, 1, mdw))
				imageClient = imageServer.Client()
			})
			AfterEach(func() {
				assistedServer.Close()
				imageServer.Close()
			})

			for i := range versions {
				version := versions[i]

				It("includes nmstate for "+version.OpenshiftVersion+" "+version.CPUArchitecture, func() {
					ok, err := common.VersionGreaterOrEqual(version.OpenshiftVersion, isoeditor.MinimalVersionForNmstatectl)
					Expect(err).NotTo(HaveOccurred())

					if len(tc.expectedRamdisk) <= 0 || !ok {
						Skip(fmt.Sprintf("skipping test %s due to ocp version < 4.18 or ramdisk isn't present", tc.name))
					}

					path := fmt.Sprintf("/images/%s/pxe-initrd?version=%s&arch=%s",
						imageID, version.OpenshiftVersion, version.CPUArchitecture)
					resp2, err := imageClient.Get(imageServer.URL + path)
					Expect(err).NotTo(HaveOccurred())
					defer resp2.Body.Close()
					Expect(resp2.StatusCode).To(Equal(http.StatusOK))

					initrdBytes, err := io.ReadAll(resp2.Body)
					Expect(err).NotTo(HaveOccurred())

					nmPath, exists, err := imageStore.NmstatectlPathForParams(version.OpenshiftVersion, version.CPUArchitecture)
					Expect(err).NotTo(HaveOccurred())
					Expect(exists).To(BeTrue())
					nmBytes, err := os.ReadFile(nmPath)
					Expect(err).NotTo(HaveOccurred())

					// nmstate archive is appended last when ramdisk is present
					Expect(bytes.HasSuffix(initrdBytes, nmBytes)).To(BeTrue(), "initrd should end with nmstate archive")
				})
			}
		})
	}
})

var _ = BeforeSuite(func() {
	var err error

	imageDir, err = os.MkdirTemp("", "imagesTest")
	Expect(err).To(BeNil())

	executer := &isoeditor.CommonExecuter{}
	nmstatectlExtractorFactory := isoeditor.NewNmstatectlExtractorFactory(executer)
	nmstateHandler := isoeditor.NewNmstateHandler(imageDir, executer, nmstatectlExtractorFactory)
	imageStore, err = imagestore.NewImageStore(isoeditor.NewEditor(imageDir, nmstateHandler), imageDir, imageServiceBaseURL, false, versions, "", map[string]string{}, map[string]string{}, nmstateHandler)
	Expect(err).NotTo(HaveOccurred())

	err = imageStore.Populate(context.Background())
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	Expect(os.RemoveAll(imageDir)).To(Succeed())
})

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration testing in short mode")
		return
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "image building tests")
}

func ignitionPayloadReader(fs filesystem.FileSystem, version imagestore.OSImage) (io.ReadCloser, error) {
	ignInfoFile, err := fs.OpenFile("/coreos/igninfo.json", os.O_RDONLY)
	if err == nil {
		defer ignInfoFile.Close()

		var ignInfoBytes []byte
		ignInfoBytes, err = io.ReadAll(ignInfoFile)
		if err != nil {
			return nil, err
		}
		var ignInfo struct {
			File   string `json:"file,omitempty"`
			Length int64  `json:"length,omitempty"`
			Offset int64  `json:"offset,omitempty"`
		}
		if err = json.Unmarshal(ignInfoBytes, &ignInfo); err != nil {
			return nil, err
		}

		var containerFile filesystem.File
		containerFile, err = fs.OpenFile(ignInfo.File, os.O_RDONLY)
		if err != nil {
			return nil, err
		}
		defer containerFile.Close()

		var containerBytes []byte
		containerBytes, err = io.ReadAll(containerFile)
		if err != nil {
			return nil, err
		}
		end := ignInfo.Offset + ignInfo.Length
		if ignInfo.Length == 0 || end > int64(len(containerBytes)) {
			end = int64(len(containerBytes))
		}
		ignitionBytes := bytes.TrimRight(containerBytes[ignInfo.Offset:end], "\x00")

		return io.NopCloser(bytes.NewReader(ignitionBytes)), nil
	}

	// fallback to the default path: ignition.img
	var f filesystem.File
	f, err = fs.OpenFile("/images/ignition.img", os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func readIgnitionContentFromGzCpio(r io.Reader) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	cr := cpio.NewReader(gr)
	hdr, err := cr.Next()
	if err != nil {
		return nil, err
	}
	if hdr.Name != "config.ign" {
		return nil, fmt.Errorf("unexpected cpio entry: %s", hdr.Name)
	}
	return io.ReadAll(cr)
}

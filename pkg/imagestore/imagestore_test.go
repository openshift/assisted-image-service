package imagestore

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

func TestImageStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "imagestore")
}

var _ = Describe("NewImageStore", func() {
	It("uses the default versions", func() {
		is, err := NewImageStore(nil, "", false)
		Expect(err).NotTo(HaveOccurred())

		Expect(is.(*rhcosStore).versions).To(Equal(DefaultVersions))
	})

	Context("with RHCOS_VERSIONS set", func() {
		var versions string
		AfterEach(func() {
			Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
		})

		It("initializes the versions value correctly", func() {
			versions = `[{"openshift_version": "4.8", "cpu_architecture": "x86_64", "url": "http://example.com/image/48.iso", "rootfs_url": "http://example.com/image/48.img"}]`
			Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

			is, err := NewImageStore(nil, "", false)
			Expect(err).NotTo(HaveOccurred())

			expected := []map[string]string{
				{
					"openshift_version": "4.8",
					"cpu_architecture":  "x86_64",
					"url":               "http://example.com/image/48.iso",
					"rootfs_url":        "http://example.com/image/48.img",
				},
			}
			Expect(is.(*rhcosStore).versions).To(Equal(expected))
		})

		It("should error when RHCOS_IMAGES is set as an empty slice/array", func() {
			versions = "[]"
			Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

			_, err := NewImageStore(nil, "", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("invalid versions: must not be empty"))

		})

		It("should error when openshift_version is not set", func() {
			versions = `[{"cpu_architecture": "x86_64", "url": "http://example.com/image/48.iso", "rootfs_url": "http://example.com/image/48.img"}]`
			Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

			_, err := NewImageStore(nil, "", false)
			Expect(err).To(HaveOccurred())
		})

		It("should error when cpu_architecture is not set", func() {
			versions = `[{"openshift_version": "4.8", "url": "http://example.com/image/48.iso", "rootfs_url": "http://example.com/image/48.img"}]`
			Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

			_, err := NewImageStore(nil, "", false)
			Expect(err).To(HaveOccurred())
		})

		It("should error when url is not set", func() {
			versions = `[{"openshift_version": "4.8", "cpu_architecture": "x86_64", "rootfs_url": "http://example.com/image/48.img"}]`
			Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

			_, err := NewImageStore(nil, "", false)
			Expect(err).To(HaveOccurred())
		})

		It("should error when rootfs_url is not set", func() {
			versions = `[{"openshift_version": "4.8", "cpu_architecture": "x86_64", "url": "http://example.com/image/48.iso"}]`
			Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

			_, err := NewImageStore(nil, "", false)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Context("with a data directory configured", func() {
	var (
		dataDir string
	)

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "imageStoreTest")
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a test server", func() {
		var (
			ts  *ghttp.Server
			ctx = context.Background()
		)

		BeforeEach(func() {
			ts = ghttp.NewServer()
		})

		AfterEach(func() {
			ts.Close()
			Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
		})

		Describe("Populate", func() {
			var (
				ctrl             *gomock.Controller
				mockEditor       *isoeditor.MockEditor
				versionsTemplate = `[{"openshift_version": "4.8", "cpu_architecture": "x86_64", "url": "%s", "rootfs_url": "http://example.com/image/48.img"}]`
			)

			BeforeEach(func() {
				ctrl = gomock.NewController(GinkgoT())
				mockEditor = isoeditor.NewMockEditor(ctrl)
			})

			It("downloads an image correctly", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, "someisocontenthere"),
					),
				)
				versions := fmt.Sprintf(versionsTemplate, ts.URL()+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir, false)
				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-x86_64.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("someisocontenthere"))
			})

			It("fails when the download fails", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/fail.iso"),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				versions := fmt.Sprintf(versionsTemplate, ts.URL()+"/fail.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("fails when minimal iso creation fails", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, "someisocontenthere"),
					),
				)
				versions := fmt.Sprintf(versionsTemplate, ts.URL()+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir, false)

				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(fmt.Errorf("minimal iso creation failed"))
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("doesn't download if the file already exists", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/dontcallthis.iso"),
						http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Fail("endpoint should not be queried") }),
					),
				)
				versions := fmt.Sprintf(versionsTemplate, ts.URL()+"/dontcallthis.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-x86_64.iso"), []byte("moreisocontent"), 0600)).To(Succeed())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("doesn't create the minimal iso when it's already present", func() {
				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/dontcallthis.iso"),
						http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Fail("endpoint should not be queried") }),
					),
				)
				versions := fmt.Sprintf(versionsTemplate, ts.URL()+"/dontcallthis.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-x86_64.iso"), []byte("moreisocontent"), 0600)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(dataDir, "rhcos-minimal-iso-4.8-x86_64.iso"), []byte("minimalisocontent"), 0600)).To(Succeed())

				Expect(is.Populate(ctx)).To(Succeed())
			})
		})
	})
})

var _ = Describe("PathForParams", func() {
	It("creates the correct path", func() {
		is, err := NewImageStore(nil, "/tmp/some/dir", false)
		Expect(err).NotTo(HaveOccurred())
		expected := "/tmp/some/dir/rhcos-type-version-arch.iso"
		Expect(is.PathForParams("type", "version", "arch")).To(Equal(expected))
	})
})

var _ = Describe("HaveVersion", func() {
	var (
		versions = `[
			{
				"openshift_version": "4.8",
				"cpu_architecture": "x86_64",
				"url": "http://example.com/image/x86_64-48.iso",
				"rootfs_url": "http://example.com/image/x86_64-48.img"
			},
			{
				"openshift_version": "4.9",
				"cpu_architecture": "arm64",
				"url": "http://example.com/image/arm64-49.iso",
				"rootfs_url": "http://example.com/image/arm64-49.img"
			}
		]`
		store ImageStore
	)

	BeforeEach(func() {
		Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())
		var err error
		store, err = NewImageStore(nil, "", false)
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
	})

	It("is true for versions that are present", func() {
		Expect(store.HaveVersion("4.8", "x86_64")).To(BeTrue())
		Expect(store.HaveVersion("4.9", "arm64")).To(BeTrue())
	})

	It("is false for versions that are missing", func() {
		Expect(store.HaveVersion("4.9", "x86_64")).To(BeFalse())
		Expect(store.HaveVersion("4.8", "arm64")).To(BeFalse())
		Expect(store.HaveVersion("4.7", "x86_64")).To(BeFalse())
		Expect(store.HaveVersion("4.8", "aarch64")).To(BeFalse())
	})
})

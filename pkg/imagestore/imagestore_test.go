package imagestore

import (
	"context"
	"fmt"
	"io/fs"
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

var _ = Context("with a data directory configured", func() {
	var (
		dataDir string
	)

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "imageStoreTest")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
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
				ctrl       *gomock.Controller
				mockEditor *isoeditor.MockEditor
				version    = map[string]string{
					"openshift_version": "4.8",
					"cpu_architecture":  "x86_64",
					"rootfs_url":        "http://example.com/image/48.img",
				}
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
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, []map[string]string{version})
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
				version["url"] = ts.URL() + "/fail.iso"
				is, err := NewImageStore(mockEditor, dataDir, []map[string]string{version})
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
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, []map[string]string{version})
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
				version["url"] = ts.URL() + "/dontcallthis.iso"
				is, err := NewImageStore(mockEditor, dataDir, []map[string]string{version})
				Expect(err).NotTo(HaveOccurred())

				Expect(os.WriteFile(filepath.Join(dataDir, "rhcos-full-iso-4.8-x86_64.iso"), []byte("moreisocontent"), 0600)).To(Succeed())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("recreates the minimal iso even when it's already present", func() {
				is, err := NewImageStore(mockEditor, dataDir, []map[string]string{version})
				Expect(err).NotTo(HaveOccurred())

				fullPath := filepath.Join(dataDir, "rhcos-full-iso-4.8-x86_64.iso")
				Expect(os.WriteFile(fullPath, []byte("moreisocontent"), 0600)).To(Succeed())

				minimalPath := filepath.Join(dataDir, "rhcos-minimal-iso-4.8-x86_64.iso")
				Expect(os.WriteFile(minimalPath, []byte("minimalisocontent"), 0600)).To(Succeed())

				mockEditor.EXPECT().CreateMinimalISOTemplate(fullPath, "http://example.com/image/48.img", minimalPath).Return(nil)

				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("cleans up files that are not configured isos", func() {
				oldISOPath := filepath.Join(dataDir, "rhcos-full-iso-4.7-x86_64.iso")
				Expect(os.WriteFile(oldISOPath, []byte("oldisocontent"), 0600)).To(Succeed())

				ts.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/some.iso"),
						ghttp.RespondWith(http.StatusOK, "someisocontenthere"),
					),
				)
				version["url"] = ts.URL() + "/some.iso"
				is, err := NewImageStore(mockEditor, dataDir, []map[string]string{version})
				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				_, err = os.Stat(oldISOPath)
				Expect(err).To(MatchError(fs.ErrNotExist))
			})
		})
	})
})

var _ = Describe("PathForParams", func() {
	It("creates the correct path", func() {
		is, err := NewImageStore(nil, "/tmp/some/dir", DefaultVersions)
		Expect(err).NotTo(HaveOccurred())
		expected := "/tmp/some/dir/rhcos-type-version-arch.iso"
		Expect(is.PathForParams("type", "version", "arch")).To(Equal(expected))
	})
})

var _ = Describe("HaveVersion", func() {
	var (
		versions = []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"url":               "http://example.com/image/x86_64-48.iso",
				"rootfs_url":        "http://example.com/image/x86_64-48.img",
			},
			{
				"openshift_version": "4.9",
				"cpu_architecture":  "arm64",
				"url":               "http://example.com/image/arm64-49.iso",
				"rootfs_url":        "http://example.com/image/arm64-49.img",
			},
		}
		store ImageStore
	)

	BeforeEach(func() {
		var err error
		store, err = NewImageStore(nil, "", versions)
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
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

var _ = Describe("NewImageStore", func() {

	It("should not error with valid version", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"url":               "http://example.com/image/x86_64-48.iso",
				"rootfs_url":        "http://example.com/image/x86_64-48.img",
			},
		}
		_, err := NewImageStore(nil, "", versions)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should error when RHCOS_IMAGES are not set i.e. versions is an empty slice", func() {
		versions := []map[string]string{}
		_, err := NewImageStore(nil, "", versions)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("invalid versions: must not be empty"))

	})

	It("should error when openshift_version is not set", func() {
		versions := []map[string]string{
			{
				"cpu_architecture": "x86_64",
				"url":              "http://example.com/image/x86_64-48.iso",
				"rootfs_url":       "http://example.com/image/x86_64-48.img",
			},
		}
		_, err := NewImageStore(nil, "", versions)
		Expect(err).To(HaveOccurred())
	})

	It("should error when cpu_architecture is not set", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"url":               "http://example.com/image/x86_64-48.iso",
				"rootfs_url":        "http://example.com/image/x86_64-48.img",
			},
		}
		_, err := NewImageStore(nil, "", versions)
		Expect(err).To(HaveOccurred())
	})

	It("should error when url is not set", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"rootfs_url":        "http://example.com/image/x86_64-48.img",
			},
		}
		_, err := NewImageStore(nil, "", versions)
		Expect(err).To(HaveOccurred())
	})

	It("should error when rootfs_url is not set", func() {
		versions := []map[string]string{
			{
				"openshift_version": "4.8",
				"cpu_architecture":  "x86_64",
				"url":               "http://example.com/image/x86_64-48.iso",
			},
		}
		_, err := NewImageStore(nil, "", versions)
		Expect(err).To(HaveOccurred())
	})

})

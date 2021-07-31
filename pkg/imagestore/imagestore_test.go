package imagestore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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

	Describe("NewImageStore", func() {
		It("uses the default versions", func() {
			is, err := NewImageStore(nil, dataDir)
			Expect(err).NotTo(HaveOccurred())

			Expect(is.(*rhcosStore).versions).To(Equal(DefaultVersions))
		})

		Context("with RHCOS_VERSIONS set", func() {
			var (
				versions = `{"4.8": {"iso_url": "http://example.com/image/48.iso", "rootfs_url": "http://example.com/image/48.img"}}`
			)

			BeforeEach(func() {
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())
			})
			AfterEach(func() {
				Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
			})

			It("initializes the versions value correctly", func() {
				is, err := NewImageStore(nil, dataDir)
				Expect(err).NotTo(HaveOccurred())

				expected := map[string]map[string]string{
					"4.8": {
						"iso_url":    "http://example.com/image/48.iso",
						"rootfs_url": "http://example.com/image/48.img",
					},
				}
				Expect(is.(*rhcosStore).versions).To(Equal(expected))
			})
		})
	})

	Context("with a test server", func() {
		var (
			ts  *httptest.Server
			ctx = context.Background()
		)

		BeforeEach(func() {
			ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch p := r.URL.Path; p {
				case "/some.iso":
					_, err := w.Write([]byte("someisocontenthere"))
					Expect(err).NotTo(HaveOccurred())
				case "/fail.iso":
					w.WriteHeader(http.StatusInternalServerError)
				case "/dontcallthis.iso":
					Fail("endpoint should not be queried")
				}

			}))
		})

		AfterEach(func() {
			ts.Close()
			Expect(os.Unsetenv("RHCOS_VERSIONS")).To(Succeed())
		})

		Describe("Populate", func() {
			var (
				ctrl       *gomock.Controller
				mockEditor *isoeditor.MockEditor
			)
			BeforeEach(func() {
				ctrl = gomock.NewController(GinkgoT())
				mockEditor = isoeditor.NewMockEditor(ctrl)
			})

			It("downloads an image correctly", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir)
				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "some.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("someisocontenthere"))
			})

			It("fails when the download fails", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/fail.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("fails when minimal iso creation fails", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir)
				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(fmt.Errorf("minimal iso creation failed"))
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("doesn't download if the file already exists", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/dontcallthis.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dataDir, "dontcallthis.iso"), []byte("moreisocontent"), 0600)).To(Succeed())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("doesn't create the minimal iso when it's already present", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/dontcallthis.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore(mockEditor, dataDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dataDir, "dontcallthis.iso"), []byte("moreisocontent"), 0600)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(dataDir, "minimal-dontcallthis.iso"), []byte("minimalisocontent"), 0600)).To(Succeed())

				Expect(is.Populate(ctx)).To(Succeed())
			})
		})

		Describe("BaseFile", func() {
			var (
				is         ImageStore
				ctrl       *gomock.Controller
				mockEditor *isoeditor.MockEditor
			)

			BeforeEach(func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())
				ctrl = gomock.NewController(GinkgoT())
				mockEditor = isoeditor.NewMockEditor(ctrl)

				var err error
				is, err = NewImageStore(mockEditor, dataDir)
				Expect(err).NotTo(HaveOccurred())

				mockEditor.EXPECT().CreateMinimalISOTemplate(gomock.Any(), "http://example.com/image/48.img", gomock.Any()).Return(nil)
				Expect(os.WriteFile(filepath.Join(dataDir, "minimal-some.iso"), []byte("minimalisocontent"), 0600)).To(Succeed())

				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("returns the correct file for full iso", func() {
				f, err := is.BaseFile("4.8", ImageTypeFull)
				Expect(err).NotTo(HaveOccurred())
				content, err := io.ReadAll(f)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("someisocontenthere"))
			})

			It("returns the correct file for minimal iso", func() {
				f, err := is.BaseFile("4.8", ImageTypeMinimal)
				Expect(err).NotTo(HaveOccurred())
				content, err := io.ReadAll(f)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("minimalisocontent"))
			})

			It("fails for a missing version", func() {
				_, err := is.BaseFile("3.2", ImageTypeFull)
				Expect(err).To(HaveOccurred())
			})

			It("fails for an unsupported image type", func() {
				_, err := is.BaseFile("4.8", "something")
				Expect(err).To(HaveOccurred())
			})
		})
	})
})

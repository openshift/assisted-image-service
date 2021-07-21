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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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

		Expect(os.Setenv("DATA_DIR", dataDir)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.Unsetenv("DATA_DIR")).To(Succeed())
	})

	Describe("NewImageStore", func() {
		It("initializes the data directory value", func() {
			is, err := NewImageStore()
			Expect(err).NotTo(HaveOccurred())
			Expect(is.cfg.DataDir).To(Equal(dataDir))
		})

		It("uses the default versions", func() {
			is, err := NewImageStore()
			Expect(err).NotTo(HaveOccurred())

			Expect(is.versions).To(Equal(DefaultVersions))
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
				is, err := NewImageStore()
				Expect(err).NotTo(HaveOccurred())

				expected := map[string]map[string]string{
					"4.8": {
						"iso_url":    "http://example.com/image/48.iso",
						"rootfs_url": "http://example.com/image/48.img",
					},
				}
				Expect(is.versions).To(Equal(expected))
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
			It("downloads an image correctly", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore()
				Expect(err).NotTo(HaveOccurred())
				Expect(is.Populate(ctx)).To(Succeed())

				content, err := os.ReadFile(filepath.Join(dataDir, "some.iso"))
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("someisocontenthere"))
			})

			It("fails when the download fails", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/fail.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore()
				Expect(err).NotTo(HaveOccurred())
				Expect(is.Populate(ctx)).NotTo(Succeed())
			})

			It("doesn't download if the file already exists", func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/dontcallthis.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				is, err := NewImageStore()
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dataDir, "dontcallthis.iso"), []byte("moreisocontent"), 0600)).To(Succeed())
				Expect(is.Populate(ctx)).To(Succeed())
			})
		})

		Describe("BaseFile", func() {
			var (
				is *ImageStore
			)

			BeforeEach(func() {
				versions := fmt.Sprintf(`{"4.8": {"iso_url": "%s", "rootfs_url": "http://example.com/image/48.img"}}`, ts.URL+"/some.iso")
				Expect(os.Setenv("RHCOS_VERSIONS", versions)).To(Succeed())

				var err error
				is, err = NewImageStore()
				Expect(err).NotTo(HaveOccurred())
				Expect(is.Populate(ctx)).To(Succeed())
			})

			It("returns the correct file", func() {
				f, err := is.BaseFile("4.8")
				Expect(err).NotTo(HaveOccurred())
				content, err := io.ReadAll(f)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("someisocontenthere"))
			})

			It("fails for a missing version", func() {
				_, err := is.BaseFile("3.2")
				Expect(err).To(HaveOccurred())
			})
		})
	})
})

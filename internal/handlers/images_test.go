package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/sync/semaphore"
)

var _ = Describe("ServeHTTP", func() {
	It("calls the iso handler ServeHTTP", func() {
		stubISOHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/images/test-image-id"))
			http.ServeContent(w, r, "some.iso", time.Now(), strings.NewReader("isocontent"))
		})
		stubInitrdHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Fail("initrd handler should not have been called")
		})

		imageHandler := &ImageHandler{
			iso:    stubISOHandler,
			initrd: stubInitrdHandler,
			sem:    semaphore.NewWeighted(100),
		}
		server := httptest.NewServer(imageHandler)
		client := server.Client()
		defer server.Close()

		resp, err := client.Get(fmt.Sprintf("%s/images/test-image-id", server.URL))
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal([]byte("isocontent")))
	})

	It("calls the initrd handler ServeHTTP", func() {
		stubISOHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Fail("initrd handler should not have been called")
		})
		stubInitrdHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/images/test-image-id/pxe-initrd"))
			http.ServeContent(w, r, "initrd.img", time.Now(), strings.NewReader("initrdcontent"))
		})

		imageHandler := &ImageHandler{
			iso:    stubISOHandler,
			initrd: stubInitrdHandler,
			sem:    semaphore.NewWeighted(100),
		}
		server := httptest.NewServer(imageHandler)
		client := server.Client()
		defer server.Close()

		resp, err := client.Get(fmt.Sprintf("%s/images/test-image-id/pxe-initrd", server.URL))
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal([]byte("initrdcontent")))
	})
})

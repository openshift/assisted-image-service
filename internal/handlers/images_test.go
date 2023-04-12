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
)

var _ = Describe("ServeHTTP", func() {
	It("calls the iso handler ServeHTTP", func() {
		imageID := "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		stubISOHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal(fmt.Sprintf("/images/%s", imageID)))
			http.ServeContent(w, r, "some.iso", time.Now(), strings.NewReader("isocontent"))
		})
		stubInitrdHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Fail("initrd handler should not have been called")
		})

		imageHandler := &ImageHandler{
			long:   stubISOHandler,
			initrd: stubInitrdHandler,
		}
		server := httptest.NewServer(imageHandler.router(100))
		client := server.Client()
		defer server.Close()

		resp, err := client.Get(fmt.Sprintf("%s/images/%s", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal([]byte("isocontent")))
	})

	It("calls the initrd handler ServeHTTP", func() {
		imageID := "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		stubISOHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Fail("ISO handler should not have been called")
		})
		stubInitrdHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal(fmt.Sprintf("/images/%s/pxe-initrd", imageID)))
			http.ServeContent(w, r, "initrd.img", time.Now(), strings.NewReader("initrdcontent"))
		})

		imageHandler := &ImageHandler{
			long:   stubISOHandler,
			initrd: stubInitrdHandler,
		}
		server := httptest.NewServer(imageHandler.router(100))
		client := server.Client()
		defer server.Close()

		resp, err := client.Get(fmt.Sprintf("%s/images/%s/pxe-initrd", server.URL, imageID))
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		respContent, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(respContent).To(Equal([]byte("initrdcontent")))
	})
})

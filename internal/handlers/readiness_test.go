package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ServeHTTP", func() {
	var (
		handler *ReadinessHandler
		server  *httptest.Server
		client  *http.Client
	)

	BeforeEach(func() {
		handler = NewReadinessHandler()
		server = httptest.NewServer(handler)
		client = server.Client()
	})

	AfterEach(func() {
		server.Close()
	})

	It("returns 503 when not ready", func() {
		resp, err := client.Get(fmt.Sprintf("%s/whatever", server.URL))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
	})

	It("returns 200 when ready", func() {
		handler.Enable()
		resp, err := client.Get(fmt.Sprintf("%s/whatever", server.URL))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

var _ = Describe("WithMiddleware", func() {
	var (
		handler *ReadinessHandler
		server  *httptest.Server
		client  *http.Client
	)

	teapot := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	BeforeEach(func() {
		handler = NewReadinessHandler()
		server = httptest.NewServer(handler.WithMiddleware(teapot))
		client = server.Client()
	})

	AfterEach(func() {
		server.Close()
	})

	It("returns 503 when not ready", func() {
		resp, err := client.Get(fmt.Sprintf("%s/whatever", server.URL))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
	})

	It("returns 418 when ready", func() {
		handler.Enable()
		resp, err := client.Get(fmt.Sprintf("%s/whatever", server.URL))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
	})
})

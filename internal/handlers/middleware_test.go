package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("WithCORSMiddleware", func() {
	var (
		server *httptest.Server
		client *http.Client
	)

	BeforeEach(func() {
		baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Hello!")
		})
		allowedURLs := "https://test.example.com, https://other.example.com"

		server = httptest.NewServer(WithCORSMiddleware(baseHandler, allowedURLs))
		client = server.Client()
	})

	AfterEach(func() {
		server.Close()
	})

	doRequestWithOrigin := func(method, origin string) string {
		req, err := http.NewRequest(method, server.URL, nil)
		Expect(err).NotTo(HaveOccurred())
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		return resp.Header.Get("Access-Control-Allow-Origin")
	}

	It("returns the CORS header when provided an allowed domain", func() {
		respHeaderValue := doRequestWithOrigin(http.MethodGet, "https://test.example.com")
		Expect(respHeaderValue).To(Equal("https://test.example.com"))

		respHeaderValue = doRequestWithOrigin(http.MethodGet, "https://other.example.com")
		Expect(respHeaderValue).To(Equal("https://other.example.com"))

		respHeaderValue = doRequestWithOrigin(http.MethodHead, "https://test.example.com")
		Expect(respHeaderValue).To(Equal("https://test.example.com"))

		respHeaderValue = doRequestWithOrigin(http.MethodHead, "https://other.example.com")
		Expect(respHeaderValue).To(Equal("https://other.example.com"))
	})

	It("doesn't return the CORS header when provided a forbidden domain", func() {
		respHeaderValue := doRequestWithOrigin(http.MethodGet, "https://nope.example.com")
		Expect(respHeaderValue).To(Equal(""))

		respHeaderValue = doRequestWithOrigin(http.MethodHead, "https://nope.example.com")
		Expect(respHeaderValue).To(Equal(""))
	})

	It("doesn't return the CORS header when the origin header is missing", func() {
		respHeaderValue := doRequestWithOrigin(http.MethodGet, "")
		Expect(respHeaderValue).To(Equal(""))

		respHeaderValue = doRequestWithOrigin(http.MethodHead, "")
		Expect(respHeaderValue).To(Equal(""))
	})
})

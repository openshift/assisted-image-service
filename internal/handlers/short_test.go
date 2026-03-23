package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parse short URLs", func() {
	var (
		imageID = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"

		// generated at https://jwt.io/ with payload:
		//
		//	{
		//		"infra_env_id": "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		//	}
		tokenInfraEnv = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJiZjI1MjkyYS1kZGRkLTQ5ZGMtYWI5Yy0zZmI0YzFmMDcwNzEifQ.VbI4JtIVcxy2S7n2tYFgtFPD9St15RrzQnpJuE0CuAI" //#nosec

		// generated at https://jwt.io/ with payload:
		//
		//	{
		//		"sub": "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		//	}
		tokenSub = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJiZjI1MjkyYS1kZGRkLTQ5ZGMtYWI5Yy0zZmI0YzFmMDcwNzEifQ.VbI4JtIVcxy2S7n2tYFgtFPD9St15RrzQnpJuE0CuAI" //#nosec

		// generated at https://jwt.io/ with payload:
		//
		//	{
		//	 	"name": "John Doe"
		//	}
		tokenNoID = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJuYW1lIjoiSm9obiBEb2UifQ.DjwRE2jZhren2Wt37t5hlVru6Myq4AhpGLiiefF69u8" //#nosec
	)

	Describe("idFromJWT tests", func() {
		It("succeeds with a  JWT with infra env ID", func() {
			value, err := idFromJWT(tokenInfraEnv)

			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal(imageID))
		})

		It("succeeds with a JWT with sub ID", func() {
			value, err := idFromJWT(tokenSub)

			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal(imageID))
		})

		It("returns an error when parsing an invalid JWT", func() {
			_, err := idFromJWT("not a JWT")

			Expect(err).To(HaveOccurred())
		})

		It("returns an error when parsing a JWT that can't be base64 decoded", func() {
			_, err := idFromJWT("foo.bar.baz")

			Expect(err).To(HaveOccurred())
		})

		It("returns an error when parsing a JWT with no ID", func() {
			_, err := idFromJWT(tokenNoID)

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("parseShortURL tests", func() {
		It("200 if infra_env_id present in token", func() {
			r := requestWithKeys(tokenInfraEnv, "", "4.12", "x86_64", "full.iso")

			params, _, err := parseShortURL(r)

			Expect(err).NotTo(HaveOccurred())
			Expect(params.imageID).To(Equal(imageID))
			Expect(params.imageType).To(Equal("full-iso"))
			Expect(params.version).To(Equal("4.12"))
			Expect(params.arch).To(Equal("x86_64"))
		})
		It("200 for disconnected.iso file", func() {
			r := requestWithKeys(tokenInfraEnv, "", "4.12", "x86_64", "disconnected.iso")

			params, _, err := parseShortURL(r)

			Expect(err).NotTo(HaveOccurred())
			Expect(params.imageID).To(Equal(imageID))
			Expect(params.imageType).To(Equal("disconnected-iso"))
			Expect(params.version).To(Equal("4.12"))
			Expect(params.arch).To(Equal("x86_64"))
		})
		It("200 if sub present in token", func() {
			r := requestWithKeys(tokenSub, "", "4.12", "x86_64", "full.iso")

			params, _, err := parseShortURL(r)

			Expect(err).NotTo(HaveOccurred())
			Expect(params.imageID).To(Equal(imageID))
			Expect(params.version).To(Equal("4.12"))
			Expect(params.arch).To(Equal("x86_64"))
		})
		It("200 if imageID present in URL", func() {
			r := requestWithKeys("", imageID, "4.12", "x86_64", "minimal.iso")

			params, _, err := parseShortURL(r)

			Expect(err).NotTo(HaveOccurred())
			Expect(params.imageID).To(Equal(imageID))
			Expect(params.version).To(Equal("4.12"))
			Expect(params.arch).To(Equal("x86_64"))
		})
		It("404 if image ID not present in token", func() {
			r := requestWithKeys(tokenNoID, "", "4.12", "x86_64", "full.iso")

			_, code, err := parseShortURL(r)

			Expect(code).To(Equal(http.StatusNotFound))
			Expect(err).To(HaveOccurred())
		})
		It("404 if file name not recognized", func() {
			r := requestWithKeys(tokenNoID, "", "4.12", "x86_64", "entire.iso")

			_, code, err := parseShortURL(r)

			Expect(code).To(Equal(http.StatusNotFound))
			Expect(err).To(HaveOccurred())
		})
	})
})

func requestWithKeys(token, imageID, version, arch, filename string) *http.Request {
	url := ""
	rctx := chi.NewRouteContext()
	if token != "" {
		url = fmt.Sprintf("https://example.redhat.com/bytoken/%s/%s/%s/%s", token, version, arch, filename)
		rctx.URLParams.Add("token", token)
	}
	if imageID != "" {
		url = fmt.Sprintf("https://example.redhat.com/byid/%s/%s/%s/%s", imageID, version, arch, filename)
		rctx.URLParams.Add("image_id", imageID)
	}
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("arch", arch)
	rctx.URLParams.Add("filename", filename)
	r := httptest.NewRequest(http.MethodGet, url, strings.NewReader(""))
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

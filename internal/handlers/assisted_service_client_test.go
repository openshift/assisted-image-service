package handlers

import (
	"fmt"
	"net/http"
	"net/url"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("AssistedServiceClient", func() {

	It("should fail with an error when trying to create new client without ASSISTED_SERVICE_HOST set", func() {
		_, err := NewAssistedServiceClient("http", "", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("ASSISTED_SERVICE_HOST is not set"))
	})

	Describe("discoveryKernelArguments", func() {
		var (
			assistedServer *ghttp.Server
			client         *AssistedServiceClient
			infraEnvID     = "bf25292a-dddd-49dc-ab9c-3fb4c1f07071"
		)

		BeforeEach(func() {
			assistedServer = ghttp.NewServer()
			u, err := url.Parse(assistedServer.URL())
			Expect(err).NotTo(HaveOccurred())
			client, err = NewAssistedServiceClient(u.Scheme, u.Host, "")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			assistedServer.Close()
		})

		getKargs := func(infraEnvJSON string) ([]byte, error) {
			assistedServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", fmt.Sprintf(infraEnvPathFormat, infraEnvID)),
					ghttp.RespondWith(http.StatusOK, infraEnvJSON),
				),
			)
			req, err := http.NewRequest("GET", "/", nil)
			Expect(err).NotTo(HaveOccurred())
			kargs, _, err := client.discoveryKernelArguments(req, infraEnvID)
			return kargs, err
		}

		DescribeTable("builds the kernel arguments from the infra-env",
			func(infraEnvJSON string, expected []byte) {
				kargs, err := getKargs(infraEnvJSON)
				Expect(err).NotTo(HaveOccurred())
				Expect(kargs).To(Equal(expected))
			},
			Entry("no kernel arguments and no FIPS",
				`{}`, nil),
			Entry("FIPS disabled",
				`{"fips_enabled": false}`, nil),
			Entry("kernel arguments only",
				`{"kernel_arguments": "[{\"operation\":\"append\",\"value\":\"p1\"}]"}`, []byte(" p1\n")),
			Entry("FIPS only",
				`{"fips_enabled": true}`, []byte(" fips=1\n")),
			Entry("FIPS appended after the user's kernel arguments",
				`{"fips_enabled": true, "kernel_arguments": "[{\"operation\":\"append\",\"value\":\"p1\"}]"}`, []byte(" p1 fips=1\n")),
			Entry("FIPS not duplicated when the user already added it",
				`{"fips_enabled": true, "kernel_arguments": "[{\"operation\":\"append\",\"value\":\"fips=1\"}]"}`, []byte(" fips=1\n")),
			Entry("FIPS disabled leaves the user's kernel arguments unchanged",
				`{"fips_enabled": false, "kernel_arguments": "[{\"operation\":\"append\",\"value\":\"p1\"}]"}`, []byte(" p1\n")),
		)

		It("fails when the kernel arguments cannot be parsed, even with FIPS enabled", func() {
			_, err := getKargs(`{"fips_enabled": true, "kernel_arguments": "not-json"}`)
			Expect(err).To(HaveOccurred())
		})
	})
})

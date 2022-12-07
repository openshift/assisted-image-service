package handlers

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AssistedServiceClient", func() {

	It("should fail with an error when trying to create new client without ASSISTED_SERVICE_HOST set", func() {
		_, err := NewAssistedServiceClient("http", "", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("ASSISTED_SERVICE_HOST is not set"))
	})

})

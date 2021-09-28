package integration

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration testing in short mode")
		return
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "image building tests")
}

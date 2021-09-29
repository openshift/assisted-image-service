package integration

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/openshift/assisted-image-service/pkg/imagestore"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

var _ = BeforeSuite(func() {
	var err error

	imageDir, err = ioutil.TempDir("", "imagesTest")
	Expect(err).To(BeNil())
	scratchSpaceDir, err = ioutil.TempDir("", "imagesTestScratch")
	Expect(err).NotTo(HaveOccurred())

	is, err = imagestore.NewImageStore(isoeditor.NewEditor(imageDir), imageDir, versions)
	Expect(err).NotTo(HaveOccurred())

	err = is.Populate(ctxBg)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	Expect(os.RemoveAll(imageDir)).To(Succeed())
	Expect(os.RemoveAll(scratchSpaceDir)).To(Succeed())
})

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration testing in short mode")
		return
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "image building tests")
}

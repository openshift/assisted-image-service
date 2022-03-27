package handlers

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	log.SetOutput(io.Discard)
	RunSpecs(t, "handlers")
}

func createTestISO() string {
	filesDir, err := os.MkdirTemp("", "isotest")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(filesDir)

	temp, err := os.CreateTemp("", "handlers-test")
	Expect(err).ToNot(HaveOccurred())

	isoFile := temp.Name()
	Expect(os.MkdirAll(filepath.Join(filesDir, "images/pxeboot"), 0755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "images/pxeboot/vmlinuz"), []byte("this is kernel"), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "images/pxeboot/initrd.img"), []byte("this is initrd"), 0600)).To(Succeed())

	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-o", isoFile, filesDir)
	Expect(cmd.Run()).To(Succeed())
	return isoFile
}

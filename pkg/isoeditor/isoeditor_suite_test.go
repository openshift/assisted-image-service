package isoeditor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIsoEditor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IsoEditor")
}

const testGrubConfig = `
menuentry 'RHEL CoreOS (Live)' --class fedora --class gnu-linux --class gnu --class os {
	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
	initrd /images/pxeboot/initrd.img /images/ignition.img
}
`

const testISOLinuxConfig = `
label linux
  menu label ^RHEL CoreOS (Live)
  menu default
  kernel /images/pxeboot/vmlinuz
  append initrd=/images/pxeboot/initrd.img,/images/ignition.img random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
`

const ignitionPaddingLength = 256 * 1024 // 256KB

func createTestFiles(volumeID string) (string, string) {
	filesDir, err := os.MkdirTemp("", "isotest")
	Expect(err).ToNot(HaveOccurred())

	temp, err := os.CreateTemp("", "*test.iso")
	Expect(err).ToNot(HaveOccurred())

	isoFile := temp.Name()
	Expect(temp.Close()).To(Succeed())
	Expect(os.Remove(temp.Name())).To(Succeed())

	Expect(os.MkdirAll(filepath.Join(filesDir, "images/pxeboot"), 0755)).To(Succeed())
	Expect(os.MkdirAll(filepath.Join(filesDir, "EFI/redhat"), 0755)).To(Succeed())
	Expect(os.MkdirAll(filepath.Join(filesDir, "isolinux"), 0755)).To(Succeed())

	// Create a file with some size to test load sector calculation
	f, err := os.Create(filepath.Join(filesDir, "images", "efiboot.img"))
	Expect(err).ToNot(HaveOccurred())
	Expect(f.Truncate(8184422)).To(Succeed())
	f, err = os.Create(filepath.Join(filesDir, "isolinux", "isolinux.bin"))
	Expect(err).ToNot(HaveOccurred())
	Expect(f.Truncate(64)).To(Succeed())

	Expect(os.WriteFile(filepath.Join(filesDir, "images/assisted_installer_custom.img"), make([]byte, RamDiskPaddingLength), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "images/ignition.img"), make([]byte, ignitionPaddingLength), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "EFI/redhat/grub.cfg"), []byte(testGrubConfig), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "isolinux/isolinux.cfg"), []byte(testISOLinuxConfig), 0600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(filesDir, "isolinux/boot.cat"), []byte(""), 0600)).To(Succeed())

	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", volumeID, "-o", isoFile, filesDir)
	Expect(cmd.Run()).To(Succeed())

	return filesDir, isoFile
}

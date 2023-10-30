package isoeditor

import (
	"crypto/rand"
	"encoding/json"
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
###################### COREOS_KARG_EMBED_AREA
	initrd /images/pxeboot/initrd.img /images/ignition.img
}
`

const testISOLinuxConfig = `
label linux
  menu label ^RHEL CoreOS (Live)
  menu default
  kernel /images/pxeboot/vmlinuz
  append initrd=/images/pxeboot/initrd.img,/images/ignition.img random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
###################### COREOS_KARG_EMBED_AREA
`

const testIgnitionInfo = `
{
  "file": "images/ignition.img"
}
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

	Expect(os.MkdirAll(filepath.Join(filesDir, "coreos"), 0755)).To(Succeed())
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

	Expect(os.WriteFile(filepath.Join(filesDir, "coreos/igninfo.json"), []byte(testIgnitionInfo), 0600)).To(Succeed())
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

// createS390TestFiles creates an ISO that resembles the ones used for the S390 architecture, in
// particular it contains a '/coreos/inginfo.json' file that indicates that the ignition is
// embedded in the '/images/cdboot.img' file instead of the usual location '/images/ignition.img'.
func createS390TestFiles(volumeID string) (tmpDir string, isoFile string) {
	// Create a temporary directory:
	tmpDir, err := os.MkdirTemp("", "isotest")
	Expect(err).ToNot(HaveOccurred())

	// Create a temporary file for the ISO:
	isoFd, err := os.CreateTemp("", "*test.iso")
	Expect(err).ToNot(HaveOccurred())
	isoFile = isoFd.Name()
	Expect(isoFd.Close()).To(Succeed())
	Expect(os.Remove(isoFile)).To(Succeed())

	// Create the '/images' directoy:
	imagesDir := filepath.Join(tmpDir, "images")
	Expect(os.MkdirAll(imagesDir, 0755)).To(Succeed())

	// Create the '/images/cdboot.img' file containing a random prefix, the ignition data, and
	// a random suffix. The random prefix and suffix are intended to make things crash loudly
	// if the code tries to read and parse that as JSON.
	cdBootFile := filepath.Join(imagesDir, "cdboot.img")
	cdBootFd, err := os.OpenFile(cdBootFile, os.O_CREATE|os.O_WRONLY, 0600)
	Expect(err).ToNot(HaveOccurred())
	randomPrefix := make([]byte, 4096)
	_, err = rand.Read(randomPrefix)
	Expect(err).ToNot(HaveOccurred())
	randomSuffix := make([]byte, 4096)
	_, err = rand.Read(randomSuffix)
	Expect(err).ToNot(HaveOccurred())
	ignitionBytes := make([]byte, ignitionPaddingLength)
	_, err = cdBootFd.Write(randomPrefix)
	Expect(err).ToNot(HaveOccurred())
	_, err = cdBootFd.Write(ignitionBytes)
	Expect(err).ToNot(HaveOccurred())
	_, err = cdBootFd.Write(randomSuffix)
	Expect(err).ToNot(HaveOccurred())
	Expect(cdBootFd.Close()).To(Succeed())

	// Create the '/coreos' directory:
	coreosDir := filepath.Join(tmpDir, "coreos")
	Expect(os.MkdirAll(coreosDir, 0755)).To(Succeed())

	// Create the '/coreos/igninfo.json' file:
	ignInf := ignitionInfo{
		File:   "images/cdboot.img",
		Offset: int64(len(randomPrefix)),
		Length: ignitionPaddingLength,
	}
	ignInfData, err := json.Marshal(ignInf)
	Expect(err).ToNot(HaveOccurred())
	ignInfFile := filepath.Join(coreosDir, "igninfo.json")
	Expect(os.WriteFile(ignInfFile, ignInfData, 0600)).To(Succeed())

	// Create the ISO:
	cmd := exec.Command(
		"genisoimage",
		"-rational-rock",
		"-J",
		"-joliet-long",
		"-V", volumeID,
		"-o", isoFile,
		tmpDir,
	)
	Expect(cmd.Run()).To(Succeed())

	return tmpDir, isoFile
}

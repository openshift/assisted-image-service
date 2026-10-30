package isoeditor

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/mock/gomock"
)

// TestErofsRhcosISO compares native extraction with an independently extracted
// binary. Large external images are supplied explicitly rather than downloaded.
func TestErofsRhcosISO(t *testing.T) {
	isoPath := os.Getenv("RHCOS_EROFS_ISO")
	if testing.Short() || isoPath == "" {
		t.Skip("set RHCOS_EROFS_ISO and RHCOS_NMSTATECTL_REFERENCE to validate a real EROFS ISO")
	}
	referencePath := os.Getenv("RHCOS_NMSTATECTL_REFERENCE")
	if referencePath == "" {
		t.Fatal("RHCOS_NMSTATECTL_REFERENCE must name an independently extracted nmstatectl")
	}
	referenceFile, err := os.Open(referencePath) //nolint:gosec // This opt-in test intentionally reads the caller's local reference file.
	if err != nil {
		t.Fatal(err)
	}
	defer referenceFile.Close()
	reference, err := io.ReadAll(referenceFile)
	if err != nil {
		t.Fatal(err)
	}
	info, err := referenceFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	rootfs, err := GetFileFromISO(isoPath, RootfsImagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer rootfs.Close()
	workDir := t.TempDir()
	rootfsPath := filepath.Join(workDir, "rootfs.img")
	output, err := os.Create(rootfsPath)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, rootfs)
	closeErr := output.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// Require an EROFS member so this test cannot accidentally validate SquashFS.
	extension, _, _, err := inspectRootfs(rootfsPath)
	if err != nil {
		t.Fatal(err)
	}
	if extension != "erofs" {
		t.Fatalf("expected an EROFS image, found %s", extension)
	}
	executer := NewMockExecuter(gomock.NewController(t))
	handler := NewNmstateHandler(workDir, executer, NewNmstatectlExtractorFactory(executer))
	archive, err := handler.BuildNmstateCpioArchive(rootfsPath)
	if err != nil {
		t.Fatal(err)
	}
	readNmstateArchive(t, archive, reference, info.Mode())
	t.Logf("ISO=%s nmstatectl size=%d mode=%v sha256=%x", isoPath, len(reference), info.Mode(), sha256.Sum256(reference))
}

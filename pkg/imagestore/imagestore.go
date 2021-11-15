package imagestore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"golang.org/x/sync/errgroup"
)

var DefaultVersions = []map[string]string{
	{
		"openshift_version": "4.6",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso",
		"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.6/4.6.8/rhcos-live-rootfs.x86_64.img",
	},
	{
		"openshift_version": "4.7",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.7/4.7.13/rhcos-4.7.13-x86_64-live.x86_64.iso",
		"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.7/4.7.13/rhcos-live-rootfs.x86_64.img",
	},
	{
		"openshift_version": "4.8",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/4.8.2/rhcos-4.8.2-x86_64-live.x86_64.iso",
		"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/4.8.2/rhcos-live-rootfs.x86_64.img",
	},
}

//go:generate mockgen -package=imagestore -destination=mock_imagestore.go . ImageStore
type ImageStore interface {
	Populate(ctx context.Context) error
	PathForParams(imageType, version, arch string) string
	HaveVersion(version, arch string) bool
}

type rhcosStore struct {
	versions  []map[string]string
	isoEditor isoeditor.Editor
	dataDir   string
}

const (
	ImageTypeFull    = "full-iso"
	ImageTypeMinimal = "minimal-iso"
)

func NewImageStore(ed isoeditor.Editor, dataDir string, versions []map[string]string) (ImageStore, error) {
	if err := validateVersions(versions); err != nil {
		return nil, err
	}
	return &rhcosStore{
		versions:  versions,
		isoEditor: ed,
		dataDir:   dataDir,
	}, nil
}

func validateVersions(versions []map[string]string) error {
	if len(versions) == 0 {
		return fmt.Errorf("invalid versions: must not be empty")
	}
	for _, entry := range versions {
		missingKeyFmt := "invalid version entry %+v: missing %s key"
		if _, ok := entry["openshift_version"]; !ok {
			return fmt.Errorf(missingKeyFmt, entry, "openshift_version")
		}
		if _, ok := entry["cpu_architecture"]; !ok {
			return fmt.Errorf(missingKeyFmt, entry, "cpu_architecture")
		}
		if _, ok := entry["url"]; !ok {
			return fmt.Errorf(missingKeyFmt, entry, "url")
		}
		if _, ok := entry["rootfs_url"]; !ok {
			return fmt.Errorf(missingKeyFmt, entry, "rootfs_url")
		}
	}

	return nil
}

func downloadURLToFile(url string, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Request to %s returned error code %d", url, resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	count, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	} else if count != resp.ContentLength {
		return fmt.Errorf("Wrote %d bytes, but expected to write %d", count, resp.ContentLength)
	}

	return nil
}

func (s *rhcosStore) Populate(ctx context.Context) error {
	if err := s.cleanDataDir(); err != nil {
		return err
	}

	errs, _ := errgroup.WithContext(ctx)

	for i := range s.versions {
		version := s.versions[i]
		errs.Go(func() error {
			openshiftVersion := version["openshift_version"]
			arch := version["cpu_architecture"]

			fullPath := s.PathForParams(ImageTypeFull, openshiftVersion, arch)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				url := version["url"]
				log.Infof("Downloading iso from %s to %s", url, fullPath)

				err = downloadURLToFile(url, fullPath)
				if err != nil {
					return fmt.Errorf("failed to download %s: %v", url, err)
				}

				log.Infof("Finished downloading for %s-%s", openshiftVersion, arch)
			}

			minimalPath := s.PathForParams(ImageTypeMinimal, openshiftVersion, arch)
			if _, err := os.Stat(minimalPath); os.IsNotExist(err) {
				log.Infof("Creating minimal iso for %s-%s", openshiftVersion, arch)

				err = s.isoEditor.CreateMinimalISOTemplate(fullPath, version["rootfs_url"], minimalPath)
				if err != nil {
					return fmt.Errorf("failed to create minimal iso template for version %s: %v", version, err)
				}

				log.Infof("Finished creating minimal iso for %s-%s", openshiftVersion, arch)
			}

			return nil
		})
	}

	return errs.Wait()
}

func (s *rhcosStore) PathForParams(imageType, version, arch string) string {
	return filepath.Join(s.dataDir, isoFileName(imageType, version, arch))
}

func isoFileName(imageType, version, arch string) string {
	return fmt.Sprintf("rhcos-%s-%s-%s.iso", imageType, version, arch)
}

func (s *rhcosStore) cleanDataDir() error {
	var expectedFiles []string
	for _, version := range s.versions {
		// Only add full isos here as we want to regenerate the minimal image on each deploy
		expectedFiles = append(expectedFiles, isoFileName(ImageTypeFull, version["openshift_version"], version["cpu_architecture"]))
	}

	dataDirFiles, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}

	for _, dataDirFile := range dataDirFiles {
		if !funk.ContainsString(expectedFiles, dataDirFile.Name()) {
			fileName := filepath.Join(s.dataDir, dataDirFile.Name())
			log.Infof("Removing %s from data directory", fileName)
			if err := os.RemoveAll(fileName); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *rhcosStore) HaveVersion(version, arch string) bool {
	for _, entry := range s.versions {
		v, versionPresent := entry["openshift_version"]
		a, archPresent := entry["cpu_architecture"]
		if versionPresent && v == version && archPresent && a == arch {
			return true
		}
	}
	return false
}

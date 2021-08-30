package imagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var DefaultVersions = []map[string]string{
	{
		"openshift_version": "4.6",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso",
		"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-live-rootfs.x86_64.img",
	},
	{
		"openshift_version": "4.7",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.13/rhcos-4.7.13-x86_64-live.x86_64.iso",
		"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.13/rhcos-live-rootfs.x86_64.img",
	},
	{
		"openshift_version": "4.8",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.8/4.8.2/rhcos-4.8.2-x86_64-live.x86_64.iso",
		"rootfs_url":        "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.8/4.8.2/rhcos-live-rootfs.x86_64.img",
	},
}

//go:generate mockgen -package=imagestore -destination=mock_imagestore.go . ImageStore
type ImageStore interface {
	Populate(ctx context.Context) error
	PathForParams(imageType, version, arch string) string
	HaveVersion(version string) bool
}

type Config struct {
	Versions string `envconfig:"RHCOS_VERSIONS"`
}

type rhcosStore struct {
	cfg       *Config
	versions  []map[string]string
	isoEditor isoeditor.Editor
	dataDir   string
}

const (
	ImageTypeFull    = "full"
	ImageTypeMinimal = "minimal"
)

func NewImageStore(ed isoeditor.Editor, dataDir string) (ImageStore, error) {
	cfg := &Config{}
	err := envconfig.Process("image-store", cfg)
	if err != nil {
		return nil, err
	}
	is := rhcosStore{
		cfg:       cfg,
		isoEditor: ed,
		dataDir:   dataDir,
	}
	if cfg.Versions == "" {
		is.versions = DefaultVersions
	} else {
		err = json.Unmarshal([]byte(cfg.Versions), &is.versions)
		if err != nil {
			return nil, err
		}
	}
	if err := validateVersions(is.versions); err != nil {
		return nil, err
	}
	return &is, nil
}

func validateVersions(versions []map[string]string) error {
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

				log.Infof("Finished downloading for version %s", version)
			}

			minimalPath := s.PathForParams(ImageTypeMinimal, openshiftVersion, arch)
			if _, err := os.Stat(minimalPath); os.IsNotExist(err) {
				log.Infof("Creating minimal iso for version %s", version)

				err = s.isoEditor.CreateMinimalISOTemplate(fullPath, version["rootfs_url"], minimalPath)
				if err != nil {
					return fmt.Errorf("failed to create minimal iso template for version %s: %v", version, err)
				}

				log.Infof("Finished creating minimal iso for version %s", version)
			}

			return nil
		})
	}

	return errs.Wait()
}

func (s *rhcosStore) PathForParams(imageType, version, arch string) string {
	return filepath.Join(s.dataDir, fmt.Sprintf("rhcos-%s-%s-%s.iso", imageType, version, arch))
}

func (s *rhcosStore) HaveVersion(version string) bool {
	for _, entry := range s.versions {
		if v, ok := entry["openshift_version"]; ok && v == version {
			return true
		}
	}
	return false
}

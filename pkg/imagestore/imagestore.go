package imagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
	"golang.org/x/sync/errgroup"
)

var DefaultVersions = map[string]map[string]string{
	"4.6": {
		"iso_url":    "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso",
		"rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-live-rootfs.x86_64.img",
	},
	"4.7": {
		"iso_url":    "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.13/rhcos-4.7.13-x86_64-live.x86_64.iso",
		"rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.13/rhcos-live-rootfs.x86_64.img",
	},
	"4.8": {
		"iso_url":    "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/pre-release/4.8.0-rc.3/rhcos-4.8.0-rc.3-x86_64-live.x86_64.iso",
		"rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/pre-release/4.8.0-rc.3/rhcos-live-rootfs.x86_64.img",
	},
}

type Config struct {
	DataDir  string `envconfig:"DATA_DIR"`
	Versions string `envconfig:"RHCOS_VERSIONS"`
}

type ImageStore struct {
	cfg      *Config
	versions map[string]map[string]string
}

func NewImageStore() (*ImageStore, error) {
	cfg := &Config{}
	err := envconfig.Process("image-store", cfg)
	if err != nil {
		return nil, err
	}
	is := ImageStore{cfg: cfg}
	if cfg.Versions == "" {
		is.versions = DefaultVersions
	} else {
		err = json.Unmarshal([]byte(cfg.Versions), &is.versions)
		if err != nil {
			return nil, err
		}
	}
	return &is, nil
}

func (s *ImageStore) Populate(ctx context.Context) error {
	errs, _ := errgroup.WithContext(ctx)

	for version := range s.versions {
		version := version
		errs.Go(func() error {
			dest, err := s.pathForVersion(version)
			if err != nil {
				return err
			}

			// bail out early if the file exists
			if _, err = os.Stat(dest); !os.IsNotExist(err) {
				return nil
			}

			url := s.versions[version]["iso_url"]
			log.Printf("Downloading iso for version %s from %s to %s", version, url, dest)
			resp, err := http.Get(url)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode > 299 {
				return fmt.Errorf("Request to %s returned error code %d", url, resp.StatusCode)
			}

			f, err := os.Create(dest)
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
			log.Printf("Finished downloading for version %s", version)
			return nil
		})
	}

	return errs.Wait()
}

func (s *ImageStore) pathForVersion(version string) (string, error) {
	v, ok := s.versions[version]
	if !ok {
		return "", fmt.Errorf("missing version entry for %s", version)
	}
	url, ok := v["iso_url"]
	if !ok {
		return "", fmt.Errorf("version %s missing key 'iso_url'", version)
	}
	return filepath.Join(s.cfg.DataDir, filepath.Base(url)), nil
}

func (s *ImageStore) BaseFile(version string) (*os.File, error) {
	path, err := s.pathForVersion(version)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

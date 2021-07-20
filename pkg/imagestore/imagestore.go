package imagestore

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	DataDir string `envconfig:"DATA_DIR"`
}

type ImageStore struct {
	cfg      *Config
	versions map[string]map[string]string
}

func NewImageStore(versions map[string]map[string]string) (*ImageStore, error) {
	cfg := &Config{}
	err := envconfig.Process("image-store", cfg)
	if err != nil {
		return nil, err
	}
	return &ImageStore{cfg: cfg, versions: versions}, nil
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

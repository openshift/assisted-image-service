package imagestore

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/google/renameio"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	log "github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"golang.org/x/sync/errgroup"
)

var DefaultVersions = []map[string]string{
	{
		"openshift_version": "4.7",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.7/4.7.33/rhcos-4.7.33-x86_64-live.x86_64.iso",
		"version":           "47.84.202109241831-0",
	},
	{
		"openshift_version": "4.8",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/4.8.14/rhcos-4.8.14-x86_64-live.x86_64.iso",
		"version":           "48.84.202109241901-0",
	},
	{
		"openshift_version": "4.9",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.9/4.9.0/rhcos-4.9.0-x86_64-live.x86_64.iso",
		"version":           "49.84.202110081407-0",
	},
	{
		"openshift_version": "4.9",
		"cpu_architecture":  "arm64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.9/4.9.0/rhcos-4.9.0-aarch64-live.aarch64.iso",
		"version":           "49.84.202110080947-0",
	},
	{
		"openshift_version": "4.10",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.10/4.10.3/rhcos-4.10.3-x86_64-live.x86_64.iso",
		"version":           "410.84.202201251210-0",
	},
	{
		"openshift_version": "4.10",
		"cpu_architecture":  "arm64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.10/4.10.3/rhcos-4.10.3-aarch64-live.aarch64.iso",
		"version":           "410.84.202201251210-0",
	},
	{
		"openshift_version": "4.11",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/4.11.0-0.nightly-2022-04-16-163450/rhcos-4.11.0-0.nightly-2022-04-16-163450-x86_64-live.x86_64.iso",
		"version":           "411.85.202203242008-0",
	},
	{
		"openshift_version": "4.11",
		"cpu_architecture":  "arm64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/pre-release/4.11.0-0.nightly-arm64-2022-04-19-171931/rhcos-4.11.0-0.nightly-arm64-2022-04-19-171931-aarch64-live.aarch64.iso",
		"version":           "411.86.202204190940-0",
	},
}

//go:generate mockgen -package=imagestore -destination=mock_imagestore.go . ImageStore
type ImageStore interface {
	Populate(ctx context.Context) error
	PathForParams(imageType, version, arch string) string
	HaveVersion(version, arch string) bool
}

type rhcosStore struct {
	versions            []map[string]string
	isoEditor           isoeditor.Editor
	dataDir             string
	httpClient          *http.Client
	imageServiceBaseURL string
}

const (
	ImageTypeFull    = "full-iso"
	ImageTypeMinimal = "minimal-iso"
)

func NewImageStore(ed isoeditor.Editor, dataDir, imageServiceBaseURL string, insecureSkipVerify bool, versions []map[string]string) (ImageStore, error) {
	if err := validateVersions(versions); err != nil {
		return nil, err
	}
	transportConfig, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("expected http.DefaultTransport to be of type *http.Transport")
	}
	myTransport := transportConfig.Clone()
	myTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureSkipVerify} //nolint:gosec // Optionally ignore TLS (G402 error)
	httpClient := &http.Client{Transport: myTransport}

	return &rhcosStore{
		versions:            versions,
		isoEditor:           ed,
		dataDir:             dataDir,
		httpClient:          httpClient,
		imageServiceBaseURL: imageServiceBaseURL,
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
		if _, ok := entry["version"]; !ok {
			return fmt.Errorf(missingKeyFmt, entry, "version")
		}
	}

	return nil
}

func (s *rhcosStore) downloadURLToFile(url string, path string) error {
	resp, err := s.httpClient.Get(url)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("request to %s returned error code %d", url, resp.StatusCode)
	}

	t, err := renameio.TempFile("", path)
	if err != nil {
		return fmt.Errorf("unable to create a temp file for %s: %v", path, err)
	}

	defer func() {
		if err1 := t.Cleanup(); err1 != nil {
			log.WithError(err1).Errorf("Unable to clean up temp file %s", t.Name())
		}
	}()

	count, err := io.Copy(t, resp.Body)
	if err != nil {
		return err
	} else if count != resp.ContentLength {
		return fmt.Errorf("wrote %d bytes, but expected to write %d", count, resp.ContentLength)
	}

	if err := t.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("unable to atomically replace %s with temp file %s: %v", path, t.Name(), err)
	}

	return nil
}

func (s *rhcosStore) Populate(ctx context.Context) error {
	if err := s.cleanDataDir(); err != nil {
		return err
	}

	errs, _ := errgroup.WithContext(ctx)

	for i := range s.versions {
		imageInfo := s.versions[i]
		errs.Go(func() error {
			openshiftVersion := imageInfo["openshift_version"]
			imageVersion := imageInfo["version"]
			arch := imageInfo["cpu_architecture"]

			fullPath := filepath.Join(s.dataDir, isoFileName(ImageTypeFull, openshiftVersion, imageVersion, arch))
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				url := imageInfo["url"]
				log.Infof("Downloading iso from %s to %s", url, fullPath)

				err = s.downloadURLToFile(url, fullPath)
				if err != nil {
					return fmt.Errorf("failed to download %s: %v", url, err)
				}

				log.Infof("Finished downloading for %s-%s (%s)", openshiftVersion, arch, imageVersion)
			}

			return nil
		})
	}

	if err := errs.Wait(); err != nil {
		return err
	}

	for i := range s.versions {
		imageInfo := s.versions[i]
		openshiftVersion := imageInfo["openshift_version"]
		imageVersion := imageInfo["version"]
		arch := imageInfo["cpu_architecture"]

		minimalPath := filepath.Join(s.dataDir, isoFileName(ImageTypeMinimal, openshiftVersion, imageVersion, arch))
		if _, err := os.Stat(minimalPath); os.IsNotExist(err) {
			log.Infof("Creating minimal iso for %s-%s-%s", openshiftVersion, imageVersion, arch)

			fullPath := filepath.Join(s.dataDir, isoFileName(ImageTypeFull, openshiftVersion, imageVersion, arch))
			rootfsURL, err := buildRootfsURL(s.imageServiceBaseURL, arch, openshiftVersion)
			if err != nil {
				return fmt.Errorf("failed to build rootfs URL: %v", err)
			}

			err = s.isoEditor.CreateMinimalISOTemplate(fullPath, rootfsURL, minimalPath)
			if err != nil {
				return fmt.Errorf("failed to create minimal iso template for version %s: %v", imageInfo, err)
			}

			log.Infof("Finished creating minimal iso for %s-%s (%s)", openshiftVersion, arch, imageVersion)
		}
	}

	return nil
}

func (s *rhcosStore) PathForParams(imageType, openshiftVersion, arch string) string {
	var version string
	for _, entry := range s.versions {
		if entry["openshift_version"] == openshiftVersion && entry["cpu_architecture"] == arch {
			version = entry["version"]
		}
	}
	return filepath.Join(s.dataDir, isoFileName(imageType, openshiftVersion, version, arch))
}

func isoFileName(imageType, openshiftVersion, version, arch string) string {
	return fmt.Sprintf("rhcos-%s-%s-%s-%s.iso", imageType, openshiftVersion, version, arch)
}

func buildRootfsURL(baseURL, arch, version string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	downloadURL := url.URL{
		Scheme: base.Scheme,
		Host:   base.Host,
		Path:   path.Join(base.Path, "/boot-artifacts/rootfs"),
	}
	queryValues := url.Values{}
	queryValues.Set("arch", arch)
	queryValues.Set("version", version)
	downloadURL.RawQuery = queryValues.Encode()
	return downloadURL.String(), nil
}

func (s *rhcosStore) cleanDataDir() error {
	var expectedFiles []string
	for _, version := range s.versions {
		// Only add full isos here as we want to regenerate the minimal image on each deploy
		expectedFiles = append(expectedFiles, isoFileName(ImageTypeFull, version["openshift_version"], version["version"], version["cpu_architecture"]))
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

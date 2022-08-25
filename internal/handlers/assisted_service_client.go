package handlers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

type AssistedServiceClient struct {
	assistedServiceScheme string
	assistedServiceHost   string
	client                *http.Client
}

const fileRouteFormat = "/api/assisted-install/v2/infra-envs/%s/downloads/files"

func NewAssistedServiceClient(assistedServiceScheme, assistedServiceHost, caCertFile string) (*AssistedServiceClient, error) {
	client := &http.Client{}
	if caCertFile != "" {
		caCert, err := ioutil.ReadFile(caCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open cert file %s, %s", caCertFile, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append cert %s, %s", caCertFile, err)
		}

		t := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS12,
			},
		}
		client.Transport = t
	}

	return &AssistedServiceClient{
		assistedServiceScheme: assistedServiceScheme,
		assistedServiceHost:   assistedServiceHost,
		client:                client,
	}, nil
}

// ignitionContent returns the ramdisk data on success and the error and the corresponding http status code
// The code is also returned to ensure issues with authentication from the assisted service request are communicated back to the image service user
// The returned code should only be used if an error is also returned
func (c *AssistedServiceClient) ramdiskContent(imageServiceRequest *http.Request, imageID string) ([]byte, int, error) {
	var ramdiskBytes []byte
	if c.assistedServiceHost == "" {
		return nil, 0, nil
	}

	u := url.URL{
		Scheme: c.assistedServiceScheme,
		Host:   c.assistedServiceHost,
		Path:   fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/minimal-initrd", imageID),
	}
	req, err := http.NewRequestWithContext(imageServiceRequest.Context(), "GET", u.String(), nil)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	setRequestAuth(imageServiceRequest, req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("request to %s returned status %d", u.String(), resp.StatusCode)
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil, 0, nil
	}

	ramdiskBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read response body: %v", err)
	}

	return ramdiskBytes, 0, nil
}

// ignitionContent returns the ignition data on success and the error and the corresponding http status code
// The code is also returned to ensure issues with authentication from the assisted service request are communicated back to the image service user
// The returned code should only be used if an error is also returned
func (c *AssistedServiceClient) ignitionContent(imageServiceRequest *http.Request, imageID string, imageType string) (*isoeditor.IgnitionContent, string, int, error) {
	if c.assistedServiceHost == "" {
		return nil, "", 0, nil
	}

	u := url.URL{
		Scheme: c.assistedServiceScheme,
		Host:   c.assistedServiceHost,
		Path:   fmt.Sprintf(fileRouteFormat, imageID),
	}
	queryValues := url.Values{}
	queryValues.Set("file_name", "discovery.ign")
	if imageType != "" {
		queryValues.Set("discovery_iso_type", imageType)
	}
	u.RawQuery = queryValues.Encode()

	req, err := http.NewRequestWithContext(imageServiceRequest.Context(), "GET", u.String(), nil)
	if err != nil {
		return nil, "", http.StatusInternalServerError, err
	}
	setRequestAuth(imageServiceRequest, req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", http.StatusInternalServerError, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", resp.StatusCode, fmt.Errorf("ignition request to %s returned status %d", req.URL.String(), resp.StatusCode)
	}
	ignitionBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", http.StatusInternalServerError, fmt.Errorf("failed to read response body: %v", err)
	}

	return &isoeditor.IgnitionContent{Config: ignitionBytes}, resp.Header.Get("Last-Modified"), 0, nil
}

func setRequestAuth(imageRequest, assistedRequest *http.Request) {
	queryValues := imageRequest.URL.Query()
	authHeader := imageRequest.Header.Get("Authorization")

	if queryValues.Get("api_key") != "" {
		params := assistedRequest.URL.Query()
		params.Set("api_key", queryValues.Get("api_key"))
		assistedRequest.URL.RawQuery = params.Encode()
	} else if queryValues.Get("image_token") != "" {
		assistedRequest.Header.Set("Image-Token", queryValues.Get("image_token"))
	} else if authHeader != "" {
		assistedRequest.Header.Set("Authorization", authHeader)
	}
}

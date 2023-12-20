package handlers

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/openshift/assisted-image-service/pkg/isoeditor"
)

type AssistedServiceClient struct {
	assistedServiceScheme string
	assistedServiceHost   string
	client                *http.Client
}

const fileRouteFormat = "/api/assisted-install/v2/infra-envs/%s/downloads/files"

func NewAssistedServiceClient(assistedServiceScheme, assistedServiceHost string, caCertPool *x509.CertPool) (*AssistedServiceClient, error) {
	if len(assistedServiceHost) == 0 {
		return nil, fmt.Errorf("ASSISTED_SERVICE_HOST is not set")
	}
	client := &http.Client{}
	if caCertPool != nil {
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

const infraEnvPathFormat = "/api/assisted-install/v2/infra-envs/%s"

// discoveryKernelArguments returns the kernel arguments data on success (if exists) and the error and the corresponding http status code
// The code is also returned to ensure issues with authentication from the assisted service request are communicated back to the image service user
// The returned code should only be used if an error is also returned
func (c *AssistedServiceClient) discoveryKernelArguments(imageServiceRequest *http.Request, infraEnvID string) ([]byte, int, error) {

	u := url.URL{
		Scheme: c.assistedServiceScheme,
		Host:   c.assistedServiceHost,
		Path:   fmt.Sprintf(infraEnvPathFormat, infraEnvID),
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
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("infra-env request to %s returned status %d", req.URL.String(), resp.StatusCode)
	}
	d := json.NewDecoder(resp.Body)
	var infraEnv struct {
		// JSON formatted string array representing the discovery image kernel arguments.
		KernelArguments *string `json:"kernel_arguments,omitempty"`
	}
	if err = d.Decode(&infraEnv); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to decode infra-env input: %v", err)
	}
	if infraEnv.KernelArguments != nil {
		kargs, err := isoeditor.StrToKargs(*infraEnv.KernelArguments)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		return []byte(" " + strings.Join(kargs, " ") + "\n"), 0, nil
	}
	return nil, 0, nil
}

func setRequestAuth(imageRequest, assistedRequest *http.Request) {
	queryValues := imageRequest.URL.Query()
	authHeader := imageRequest.Header.Get("Authorization")
	api_key := chi.URLParam(imageRequest, "api_key")
	token := chi.URLParam(imageRequest, "token")

	switch {
	case api_key != "":
		params := assistedRequest.URL.Query()
		params.Set("api_key", api_key)
		assistedRequest.URL.RawQuery = params.Encode()
	case queryValues.Get("api_key") != "":
		params := assistedRequest.URL.Query()
		params.Set("api_key", queryValues.Get("api_key"))
		assistedRequest.URL.RawQuery = params.Encode()
	case token != "":
		assistedRequest.Header.Set("Image-Token", token)
	case queryValues.Get("image_token") != "":
		assistedRequest.Header.Set("Image-Token", queryValues.Get("image_token"))
	case authHeader != "":
		assistedRequest.Header.Set("Authorization", authHeader)
	}
}

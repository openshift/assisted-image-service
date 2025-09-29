package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
)

var jwtPayloadRegexp = regexp.MustCompile(`^.+\.(.+)\..+`)

type payload struct {
	Sub        string `json:"sub"`          // used by OCM tokens
	InfraEnvID string `json:"infra_env_id"` // used by local auth tokens
}

// parseShortURL parses short-style URLs, where URL path segments are used to
// hierarchically identify the desired resource. The URL ordering and structure
// is defined on the router.
func parseShortURL(r *http.Request) (*imageDownloadParams, int, error) {
	imageID := chi.URLParam(r, "image_id")
	token := chi.URLParam(r, "token")
	if token == "" {
		// try the "api_key" parameter name, which is used at the /byapikey/ path.
		token = chi.URLParam(r, "api_key")
	}
	version := chi.URLParam(r, "version")
	arch := chi.URLParam(r, "arch")
	filename := chi.URLParam(r, "filename")

	var err error
	// the URL can have either the token or the image_id
	if imageID == "" {
		imageID, err = idFromJWT(token)
		if err != nil {
			return nil, http.StatusNotFound, err
		}
	}

	params := imageDownloadParams{
		imageID: imageID,
		version: version,
		arch:    arch,
	}

	switch filename {
	case "minimal.iso":
		params.imageType = "minimal-iso"
	case "full.iso":
		params.imageType = "full-iso"
	case "disconnected.iso":
		params.imageType = "disconnected-iso"
	default:
		return nil, http.StatusNotFound, fmt.Errorf("unrecognized file name %s", filename)
	}

	return &params, 0, nil
}

// idFromJWT parses the JWT payload to find the infraenv ID. No signature
// verification is done here, because we do not need to trust the payload in
// this service. The JWT will be verified and evaluated for authn and authz by
// assisted-service.
func idFromJWT(jwt string) (string, error) {
	match := jwtPayloadRegexp.FindStringSubmatch(jwt)

	if len(match) != 2 {
		return "", fmt.Errorf("failed to parse JWT from URL")
	}

	decoded, err := base64.RawStdEncoding.DecodeString(match[1])
	if err != nil {
		return "", err
	}

	var p payload
	err = json.Unmarshal(decoded, &p)
	if err != nil {
		return "", err
	}

	switch {
	case p.Sub != "":
		return p.Sub, nil
	case p.InfraEnvID != "":
		return p.InfraEnvID, nil
	}

	return "", fmt.Errorf("InfraEnv ID not found in token")
}

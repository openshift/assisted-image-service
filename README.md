# Assisted Image Service

This service customizes and serves RHCOS images for the [Assisted Installer Service](https://github.com/openshift/assisted-service).
It downloads a set of RHCOS images on startup based on config and responds to a single API endpoint to allow a user to download a customized image for use with assisted service.

## Running the Image Service

Build the image and run it locally using `podman`

```bash
make build run
```

This will start the service running on port 8080 by default.
It will also bind-mount the `data` and `certs` local directories into the container root.

## Running tests

```bash
skipper make test
```

## Configuration

- `DATA_DIR` - Path at which to store downloaded RHCOS images.
- `RHCOS_VERSIONS` - JSON string indicating the supported versions and their required urls.
- `LISTEN_PORT` - Image Service listen port
- `HTTPS_KEY_FILE` - tls key file path
- `HTTPS_CERT_FILE` - tls cert file path
- `ASSISTED_SERVICE_SCHEME` - protocol to use to query assisted service for image information
- `ASSISTED_SERVICE_HOST` - host or host:port to use to query assisted service for image information
- `REQUEST_AUTH_TYPE` - determines how the auth token should be passed to the assisted service - either `header` or `param`
  - For `header` a header of the form `Authorization: Bearer <token>` is used
  - For `param` an `api_key=<token>` query parameter is added to the URL

Example `RHCOS_VERSIONS`:
```json
{
	"4.6": {
		"iso_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso",
		"rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-live-rootfs.x86_64.img"
	},
	"4.7": {
		"iso_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.13/rhcos-4.7.13-x86_64-live.x86_64.iso",
		"rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.13/rhcos-live-rootfs.x86_64.img"
	},
	"4.8": {
		"iso_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/pre-release/4.8.0-rc.3/rhcos-4.8.0-rc.3-x86_64-live.x86_64.iso",
		"rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/pre-release/4.8.0-rc.3/rhcos-live-rootfs.x86_64.img"
	}
}
```

## API

### `GET /images/{image_id}`

Downloads the RHCOS image for the specified image ID.

#### Query paraeters

`version`: indicates the version of the RHCOS base image to use (must match a key in `RHCOS_VERSIONS`)
`type`: `full` to download the ISO including the rootfs, `minimal` to download the iso without the rootfs
`api_key`: the api token to pass through to the assisted service calls if authentication is required

### `GET /health`

Returns 200 if the service is ready to respond to requests

### `GET /metrics`

Prometheus metrics scraping endpoint

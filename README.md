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

- `ALLOWED_DOMAINS` - When set, determines how the service responds to requests with `Origin` headers
- `ASSISTED_SERVICE_HOST` - host or host:port to use to query assisted service for image information
- `ASSISTED_SERVICE_SCHEME` - protocol to use to query assisted service for image information
- `DATA_DIR` - Path at which to store downloaded RHCOS images.
- `HTTPS_CERT_FILE` - tls cert file path
- `HTTPS_KEY_FILE` - tls key file path
- `HTTP_LISTEN_PORT` - When set, plain http listener is started on that port
- `IMAGE_SERVICE_BASE_URL` - the base URL to use to query the image service
- `LISTEN_PORT` - Image Service listen port
- `LOG_LEVEL` - log level, such as "info" or "debug"; see logrus docs for a complete list
- `MAX_CONCURRENT_REQUESTS` - caps the number of inflight image downloads to avoid things like open file limits
- `RHCOS_VERSIONS`/`OS_IMAGES` - JSON string indicating the supported versions and their required urls. `OS_IMAGES` takes precedence.

Example `OS_IMAGES`:
```json
[
	{
		"openshift_version": "4.6",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso",
		"version":           "46.82.202012051820-0"
	},
	{
		"openshift_version": "4.7",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.7/4.7.33/rhcos-4.7.33-x86_64-live.x86_64.iso",
		"version":           "47.84.202109241831-0"
	},
	{
		"openshift_version": "4.8",
		"cpu_architecture":  "x86_64",
		"url":               "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.8/4.8.14/rhcos-4.8.14-x86_64-live.x86_64.iso",
		"version":           "48.84.202109241901-0"
	}
]
```

## API

None of these APIs should be considered stable for end-users of assisted
installer. Users should never construct URLs that match these APIs; instead
users should obtain an ISO URL from an InfraEnv resource, as provided by
assisted installer. These APIs represent a contract between
assisted-image-service and assisted installer only.

### `GET /byid/{image_id}/{version}/{arch}/{filename}`

Downloads the RHCOS image for the specified image ID.

URL segments:
- `image_id`: ID for the image, usually the InfraEnv ID
- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `arch`: the base image cpu architecture (must match an entry in `RHCOS_VERSIONS`)
- `filename`: `full.iso` to download the ISO including the rootfs, `minimal.iso` to download the ISO without the rootfs

### `GET /bytoken/{token}/{version}/{arch}/{filename}`

Downloads the RHCOS image for the specified image ID.

URL segments:
- `token`: JWT whose payload containes either a `sub` field or `infra_env_id` field
- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `arch`: the base image cpu architecture (must match an entry in `RHCOS_VERSIONS`)
- `filename`: `full.iso` to download the ISO including the rootfs, `minimal.iso` to download the ISO without the rootfs

### `GET /byapikey/{api_key}/{version}/{arch}/{filename}`

Downloads the RHCOS image for the specified image ID.

URL segments:
- `api_key`: JWT whose payload containes either a `sub` field or `infra_env_id` field
- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `arch`: the base image cpu architecture (must match an entry in `RHCOS_VERSIONS`)
- `filename`: `full.iso` to download the ISO including the rootfs, `minimal.iso` to download the ISO without the rootfs


## Deprecated API

### `GET /images/{image_id}`

Downloads the RHCOS image for the specified image ID.

#### Query parameters

- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `arch`: the base image cpu architecture (must match an entry in `RHCOS_VERSIONS`)
- `type`: `full-iso` to download the ISO including the rootfs, `minimal-iso` to download the ISO without the rootfs
- `api_key`: the api token to pass through to the assisted service calls if local authentication is required
- `image_token`: the token to pass through to the Image-Token assisted service header if image pre-signed authentication is required

#### Headers

- `Authorization`: this header is passed directly through to assisted service requests to handle RHSSO authentication

### `GET /images/{image_id}/pxe-initrd`

Downloads the RHCOS initrd with the ignition for the specified image appended.

#### Query parameters

- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `arch`: the base image cpu architecture (must match an entry in `RHCOS_VERSIONS`)
- `api_key`: the api token to pass through to the assisted service calls if local authentication is required
- `image_token`: the token to pass through to the Image-Token assisted service header if image pre-signed authentication is required

#### Headers

- `Authorization`: this header is passed directly through to assisted service requests to handle RHSSO authentication

### `GET /images/{image_id}/s390x-initrd-addrsize`

Only for the s390x architecture. Downloads the initrd.addrsize (16 bytes) containing the psw of the initrd (8 bytes) and the size of the initrd (8 bytes)

#### Query parameters

- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `api_key`: the api token to pass through to the assisted service calls if local authentication is required
- `image_token`: the token to pass through to the Image-Token assisted service header if image pre-signed authentication is required

#### Headers

- `Authorization`: this header is passed directly through to assisted service requests to handle RHSSO authentication

### `GET /boot-artifacts/{artifact}`

Downloads the artifact specified from the ISO. Artifacts are:

- `rootfs`: rootfs.img 
- `kernel`: vmlinuz (kernel.img when arch is s390x)

#### Architecture specific artifacts
##### s390x
- `ins-file`: generic.ins

#### Query parameters

- `version`: indicates the version of the RHCOS base image to use (must match an entry in `RHCOS_VERSIONS`)
- `arch`: the base image cpu architecture (must match an entry in `RHCOS_VERSIONS`)

### `GET /health`

Returns 503 until the images are downloaded
Returns 200 if the service is ready to respond to requests

### `GET /live`

Returns 200 if the service is running

### `GET /metrics`

Prometheus metrics scraping endpoint

## Authentication

Authentication tokens are accepted in various ways to support different deployment models and assisted service authentication backends
The following table shows how each authentication input is translated when making calls to assisted service

| Image Service                 | Assisted Service          |
|-------------------------------|---------------------------|
| `api_key` query parameter     | `api_key` query parameter |
| `image_token` query parameter | `Image-Token` header      |
| `Authorization` header        | `Authorization` header    |

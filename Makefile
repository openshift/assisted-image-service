IMAGE := $(or ${IMAGE}, quay.io/edge-infrastructure/assisted-image-service:latest)
PWD = $(shell pwd)
LISTEN_PORT := $(or ${LISTEN_PORT}, 8080)

build:
	podman build -f Dockerfile.image-service . -t $(IMAGE)

build-openshift-ci-test-bin:
	./hack/setup_env.sh

lint:
	golangci-lint run -v

test:
	go test ./...

test-short:
	go test -short ./...

test-integration:
	go test ./integration_test/...

generate:
	go generate $(shell go list ./...)
	$(MAKE) format

format:
	@goimports -w -l main.go internal pkg || /bin/true

run: certs
	podman run --rm \
		-v $(PWD)/data:/data:Z -v $(PWD)/certs:/certs:Z \
		-p$(LISTEN_PORT):$(LISTEN_PORT) \
		-e LISTEN_PORT=$(LISTEN_PORT) \
		-e HTTPS_KEY_FILE=/certs/tls.key -e HTTPS_CERT_FILE=/certs/tls.crt \
		-e ASSISTED_SERVICE_SCHEME=${ASSISTED_SERVICE_SCHEME} -e ASSISTED_SERVICE_HOST=${ASSISTED_SERVICE_HOST} \
		-e REQUEST_AUTH_TYPE=${REQUEST_AUTH_TYPE} \
		-e RHCOS_VERSIONS='${RHCOS_VERSIONS}' \
		$(IMAGE)

.PHONY: certs
certs:
	openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout certs/tls.key -out certs/tls.crt -subj "/CN=localhost"

all: lint test build run

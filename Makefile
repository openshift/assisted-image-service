IMAGE := $(or ${IMAGE}, quay.io/edge-infrastructure/assisted-image-service:latest)
PWD = $(shell pwd)
LISTEN_PORT := $(or ${LISTEN_PORT}, 8080)
IMAGE_SERVICE_BASE_URL := $(or ${IMAGE_SERVICE_BASE_URL}, http://localhost:8080)
LOG_LEVEL := $(or ${LOG_LEVEL}, info)

CI ?= false
ROOT_DIR = $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
REPORTS ?= $(ROOT_DIR)/reports
COVER_PROFILE := $(or ${COVER_PROFILE},$(REPORTS)/unit_coverage.out)

build:
	podman build -f Dockerfile.image-service . -t $(IMAGE)

build-openshift-ci-test-bin:
	./hack/setup_env.sh

lint:
	golangci-lint run -v

test: $(REPORTS)
	go test -count=1 -cover -coverprofile=$(COVER_PROFILE) ./...
	$(MAKE) _coverage

_coverage:
ifeq ($(CI), true)
	COVER_PROFILE=$(COVER_PROFILE) ./hack/publish-codecov.sh
endif

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
		-e IMAGE_SERVICE_BASE_URL=${IMAGE_SERVICE_BASE_URL} \
		-e RHCOS_VERSIONS='${RHCOS_VERSIONS}' \
		-e OS_IMAGES='${OS_IMAGES}' \
		-e HTTP_LISTEN_PORT='${HTTP_LISTEN_PORT}' \
		-e LOGLEVEL='${LOG_LEVEL}' \
		$(IMAGE)

.PHONY: certs
certs:
	openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout certs/tls.key -out certs/tls.crt -subj "/CN=localhost"

all: lint test build run

$(REPORTS):
	-mkdir -p $(REPORTS)

clean:
	-rm -rf $(REPORTS)
